package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// ValidateRobotWebhookURL restricts robot credentials to their provider endpoint.
// The shared fetch policy still applies, including deployment-specific restrictions.
func ValidateRobotWebhookURL(kind, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" {
		return errors.New("invalid robot webhook URL")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("invalid robot webhook URL")
	}
	var host, path, credential string
	switch kind {
	case dto.NotifyTypeDingTalk:
		host, path, credential = "oapi.dingtalk.com", "/robot/send", "access_token"
	case dto.NotifyTypeWeCom:
		host, path, credential = "qyapi.weixin.qq.com", "/cgi-bin/webhook/send", "key"
	default:
		return errors.New("invalid robot notification type")
	}
	if u.Host != host || u.Path != path || u.RawPath != "" || len(query[credential]) != 1 || strings.TrimSpace(query.Get(credential)) == "" {
		return errors.New("invalid robot webhook URL")
	}
	if err := ValidateSSRFProtectedFetchURL(rawURL); err != nil {
		// Fetch errors can contain the URL and its credential. Do not propagate them.
		return errors.New("robot webhook URL rejected by fetch policy")
	}
	return nil
}

func sendRobotNotify(kind, webhookURL, secret string, data dto.Notify) error {
	if err := ValidateRobotWebhookURL(kind, webhookURL); err != nil {
		return err
	}
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, dto.ContentValueParam, fmt.Sprint(value), 1)
	}
	payload := struct {
		MessageType string `json:"msgtype"`
		Text        struct {
			Content string `json:"content"`
		} `json:"text"`
	}{MessageType: "text"}
	payload.Text.Content = data.Title + "\n" + content
	body, err := common.Marshal(payload)
	if err != nil {
		return errors.New("failed to encode robot notification")
	}
	if kind == dto.NotifyTypeDingTalk && secret != "" {
		u, _ := url.Parse(webhookURL)
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp + "\n" + secret))
		query := u.Query()
		query.Set("timestamp", timestamp)
		query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		u.RawQuery = query.Encode()
		webhookURL = u.String()
	}
	var response *http.Response
	if system_setting.EnableWorker() {
		response, err = DoWorkerRequest(&WorkerRequest{
			URL:     webhookURL,
			Key:     system_setting.WorkerValidKey,
			Method:  http.MethodPost,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    body,
		})
	} else {
		request, requestErr := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
		if requestErr != nil {
			return errors.New("failed to create robot notification request")
		}
		request.Header.Set("Content-Type", "application/json")
		// Never forward a robot URL credential or message to a redirect target.
		client := *GetSSRFProtectedHTTPClient()
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		client.Timeout = 15 * time.Second
		response, err = client.Do(request)
	}
	if err != nil {
		return errors.New("failed to send robot notification")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("robot notification request failed with status code: %d", response.StatusCode)
	}
	var result struct {
		Code *int `json:"errcode"`
	}
	if err := common.DecodeJson(io.LimitReader(response.Body, 64*1024), &result); err != nil || result.Code == nil {
		return errors.New("invalid robot notification response")
	}
	if *result.Code != 0 {
		// Provider messages can echo credentials and content; expose only the code.
		return fmt.Errorf("robot notification rejected with code: %d", *result.Code)
	}
	return nil
}
