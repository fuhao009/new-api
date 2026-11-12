package openai

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObserveStreamChoicesDedupesFunctionCallNames(t *testing.T) {
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	seen := make(map[string]struct{})
	var names []string

	chunks := []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"get_time","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{}"}}]}}]}`,
	}
	for _, chunk := range chunks {
		observeStreamChoices(info, chunk, seen, &names)
	}

	require.Len(t, names, 2)
	assert.Equal(t, []string{"get_weather", "get_time"}, names)
	assert.Empty(t, info.StreamStatus.ResponseOutcome(), "tool call deltas carry no finish reason")

	observeStreamChoices(info, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, seen, &names)
	assert.Equal(t, "completed", info.StreamStatus.ResponseOutcome())
	assert.False(t, info.PerformanceBusinessRejection)

	filtered := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	observeStreamChoices(filtered, `{"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`, map[string]struct{}{}, &names)
	assert.True(t, filtered.PerformanceBusinessRejection)
}

func chatStreamFixture() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		StartTime: time.Now(), IsStream: true, DisablePing: true,
		RelayFormat:         types.RelayFormatOpenAI,
		RelayMode:           relayconstant.RelayModeChatCompletions,
		ChannelMeta:         &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
		ThinkingContentInfo: relaycommon.ThinkingContentInfo{IsFirstThinkingContent: true},
	}
	info.SetEstimatePromptTokens(10)
	return info
}

func chatTestFrame(delta, extra string) string {
	return `{"id":"chatcmpl-test","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":` + delta + `}]` + extra + `}`
}

func TestOaiStreamHandlerFirstContentDoesNotWaitForNextFrame(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	frames := []string{
		chatTestFrame(`{"role":"assistant"}`, ""),
		chatTestFrame(`{"content":"first"}`, ""),
	}
	producer := make(chan error, 1)
	go func() {
		for _, frame := range frames {
			if _, err := fmt.Fprintf(writer, "data: %s\n\n", frame); err != nil {
				producer <- err
				return
			}
		}
		producer <- nil
	}()
	type result struct {
		usage *dto.Usage
		err   *types.NewAPIError
	}
	finished := make(chan result, 1)
	engine := gin.New()
	engine.POST("/chat", func(c *gin.Context) {
		usage, err := OaiStreamHandler(c, chatStreamFixture(), &http.Response{Body: reader})
		finished <- result{usage: usage, err: err}
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	// Close the pipe before server.Close even when the client gate fails.
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/chat", strings.NewReader(`{}`))
	require.NoError(t, err)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	var first string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `"content":"first"`) {
			first = line
		}
		if line == "" && first != "" {
			break
		}
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, "data: "+frames[1], first, "next upstream frame is still gated")
	require.NoError(t, <-producer)
	_, err = fmt.Fprintf(writer, "data: %s\n\ndata: [DONE]\n\n", `{"id":"chatcmpl-test","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	for scanner.Scan() {
	}
	require.NoError(t, scanner.Err())
	select {
	case settled := <-finished:
		require.Nil(t, settled.err)
		require.NotNil(t, settled.usage)
		assert.Equal(t, 11, settled.usage.TotalTokens)
	case <-ctx.Done():
		t.Fatal("handler did not finish")
	}
}

func runChatStreamFixture(t *testing.T, info *relaycommon.RelayInfo, frames []string) (*dto.Usage, []string, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	var body strings.Builder
	for _, frame := range frames {
		fmt.Fprintf(&body, "data: %s\n\n", frame)
	}
	body.WriteString("data: [DONE]\n\n")
	usage, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body.String()))})
	require.Nil(t, apiErr)
	var data []string
	for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
		if payload, ok := strings.CutPrefix(line, "data: "); ok {
			data = append(data, payload)
		}
	}
	return usage, data, recorder.Body.String()
}

func TestOaiStreamHandlerImmediateFramesPreserveUsageAndOrder(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	operation_setting.SetToolPriceForTest("stream_fn", 1)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("stream_fn") })
	first := chatTestFrame(`{"content":"A"}`, `,"vendor_extension":{"preserved":true}`)
	second := chatTestFrame(`{"content":"B"}`, "")
	finish := `{"id":"chatcmpl-test","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	usageFrame := `{"id":"chatcmpl-test","model":"gpt-4o","created":1,"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
	empty := `{"id":"chatcmpl-test","model":"gpt-4o","created":1,"choices":[]}`
	finishUsage := strings.TrimSuffix(finish, "}") + `,"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
	tool := chatTestFrame(`{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"stream_fn","arguments":"{}"}}]}`, "")
	toolUsage := chatTestFrame(`{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"stream_fn","arguments":"{}"}}]}`, `,"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}`)
	for _, test := range []struct {
		name          string
		frames        []string
		want          []string
		upstreamUsage bool
		tool          bool
	}{
		{name: "local usage", frames: []string{first, second, finish}, want: []string{first, second, finish}},
		{name: "final usage", frames: []string{first, second, finish, usageFrame}, want: []string{first, second, finish}, upstreamUsage: true},
		{name: "second-last usage", frames: []string{first, second, usageFrame, empty}, want: []string{first, second, usageFrame, empty}, upstreamUsage: true},
		{name: "middle usage", frames: []string{usageFrame, first, second, finish}, want: []string{usageFrame, first, second, finish}},
		{name: "finish with usage", frames: []string{first, second, finishUsage}, want: []string{first, second, finishUsage}, upstreamUsage: true},
		{name: "tool with usage", frames: []string{tool, toolUsage}, want: []string{tool, toolUsage}, upstreamUsage: true, tool: true},
	} {
		for _, include := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/include=%t", test.name, include), func(t *testing.T) {
				info := chatStreamFixture()
				info.ShouldIncludeUsage = include
				usage, data, _ := runChatStreamFixture(t, info, test.frames)
				require.NotNil(t, usage)
				want := append([]string(nil), test.want...)
				if test.name == "final usage" && include {
					want = append(want, usageFrame)
				}
				if !test.upstreamUsage && include {
					require.Len(t, data, len(want)+2)
					var final struct {
						Usage *dto.Usage `json:"usage"`
					}
					require.NoError(t, common.UnmarshalJsonStr(data[len(want)], &final))
					require.NotNil(t, final.Usage)
					assert.Equal(t, usage.PromptTokens, final.Usage.PromptTokens)
					assert.Equal(t, usage.CompletionTokens, final.Usage.CompletionTokens)
					data = append(data[:len(want)], data[len(want)+1:]...)
				}
				want = append(want, "[DONE]")
				assert.Equal(t, want, data)
				if test.upstreamUsage {
					assert.Equal(t, 10, usage.PromptTokens)
					assert.Equal(t, 2, usage.CompletionTokens)
					assert.Equal(t, 12, usage.TotalTokens)
				} else {
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					expected := service.ResponseText2Usage(c, "AB", "gpt-4o", 10)
					assert.Equal(t, *expected, *usage)
				}
				if test.tool {
					require.NotNil(t, info.ResponsesUsageInfo)
					require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, "stream_fn")
					assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["stream_fn"].CallCount)
				}
			})
		}
	}
}

func TestOaiStreamHandlerFormattingAndConverterFinalState(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	finish := `{"id":"chatcmpl-test","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
	for _, test := range []struct {
		name     string
		format   types.RelayFormat
		settings dto.ChannelSettings
		frames   []string
		wantText string
	}{
		{name: "force format", format: types.RelayFormatOpenAI, settings: dto.ChannelSettings{ForceFormat: true}, frames: []string{chatTestFrame(`{"content":"A"}`, `,"vendor":true`), finish}, wantText: "A"},
		{name: "thinking", format: types.RelayFormatOpenAI, settings: dto.ChannelSettings{ThinkingToContent: true}, frames: []string{chatTestFrame(`{"reasoning_content":"plan"}`, ""), chatTestFrame(`{"content":"A"}`, ""), finish}, wantText: "<think>\nplan\n</think>\nA"},
		{name: "claude", format: types.RelayFormatClaude, frames: []string{chatTestFrame(`{"role":"assistant","content":"A"}`, ""), finish}},
		{name: "gemini", format: types.RelayFormatGemini, frames: []string{chatTestFrame(`{"role":"assistant","content":"A"}`, ""), finish}},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := chatStreamFixture()
			info.RelayFormat = test.format
			info.ChannelSetting = test.settings
			if test.format == types.RelayFormatClaude {
				info.EnsureClaudeConvertInfo()
			}
			usage, data, wire := runChatStreamFixture(t, info, test.frames)
			assert.Equal(t, 12, usage.TotalTokens)
			switch test.format {
			case types.RelayFormatOpenAI:
				var text strings.Builder
				finishCount := 0
				for _, payload := range data {
					if payload == "[DONE]" {
						continue
					}
					var frame dto.ChatCompletionsStreamResponse
					require.NoError(t, common.UnmarshalJsonStr(payload, &frame))
					for _, choice := range frame.Choices {
						text.WriteString(choice.Delta.GetContentString())
						if choice.FinishReason != nil && *choice.FinishReason != "" {
							finishCount++
						}
					}
				}
				assert.Equal(t, test.wantText, text.String())
				assert.Equal(t, 1, finishCount)
				assert.Equal(t, 1, strings.Count(wire, "data: [DONE]"))
			case types.RelayFormatClaude:
				assert.Equal(t, 1, strings.Count(wire, `"type":"message_stop"`))
				assert.Equal(t, 1, strings.Count(wire, `"type":"message_delta"`))
				assert.Contains(t, wire, `"text":"A"`)
			case types.RelayFormatGemini:
				assert.Equal(t, 1, strings.Count(wire, `"finishReason":"STOP"`))
				assert.Contains(t, wire, `"text":"A"`)
			}
		})
	}
}
