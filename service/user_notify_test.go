package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A low-balance reminder goes out once, on the charge that takes a wallet or
// subscription balance below the threshold. A balance already below it earns
// nothing more until a top-up lifts it back above the line. The site switch
// and the user's switch both turn reminders off; a user who never chose gets
// them.
func TestQuotaReminderFiresOncePerDrop(t *testing.T) {
	previousEnabled, previousThreshold := common.QuotaRemindEnabled, common.QuotaRemindThreshold
	t.Cleanup(func() {
		common.QuotaRemindEnabled, common.QuotaRemindThreshold = previousEnabled, previousThreshold
	})
	common.QuotaRemindThreshold = 1000
	on, off := true, false
	// changes move the balance from start: negative ones are charges, which
	// may remind; positive ones are top-ups. want holds one entry per charge.
	for _, tc := range []struct {
		name        string
		siteEnabled bool
		setting     dto.UserSetting
		start       int64
		changes     []int64
		want        []bool
	}{
		{
			name:        "never chosen: one reminder per drop, again after a top-up",
			siteEnabled: true,
			start:       1500,
			changes:     []int64{-400, -200, -300, +1500, -1200},
			want:        []bool{false, true, false, true},
		},
		{
			name:        "switched on with its own threshold",
			siteEnabled: true,
			setting:     dto.UserSetting{QuotaWarningEnabled: &on, QuotaWarningThreshold: 5000},
			start:       6000,
			changes:     []int64{-500, -600, -2000},
			want:        []bool{false, true, false},
		},
		{
			name:        "a charge that lands exactly on the threshold does not remind",
			siteEnabled: true,
			start:       1200,
			changes:     []int64{-200, -1},
			want:        []bool{false, true},
		},
		{
			name:        "user switched off",
			siteEnabled: true,
			setting:     dto.UserSetting{QuotaWarningEnabled: &off},
			start:       1500,
			changes:     []int64{-600},
			want:        []bool{false},
		},
		{
			name:    "site switched off",
			setting: dto.UserSetting{QuotaWarningEnabled: &on},
			start:   1500,
			changes: []int64{-600},
			want:    []bool{false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.QuotaRemindEnabled = tc.siteEnabled
			got := make([]bool, 0, len(tc.want))
			balance := tc.start
			for _, change := range tc.changes {
				if change < 0 {
					got = append(got, quotaReminderDue(tc.setting, balance, balance+change))
				}
				balance += change
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// Channel notices go to every enabled administrator whose channel switch is
// on, in that administrator's saved language. Until an administrator chooses,
// root receives them and other administrators do not; other users never do.
func TestChannelNoticesFollowEachAdministratorsSwitch(t *testing.T) {
	previousDB := model.DB
	previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	previousNotifyLimit := constant.NotifyLimitCount
	previousClient, previousWorker := httpClient, system_setting.WorkerUrl
	fetch := system_setting.GetFetchSetting()
	previousFetch := *fetch
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis
		constant.NotifyLimitCount = previousNotifyLimit
		httpClient, system_setting.WorkerUrl = previousClient, previousWorker
		*fetch = previousFetch
	})
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.User{}))
	model.DB = database
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	constant.NotifyLimitCount = 100

	var mutex sync.Mutex
	received := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload WebhookPayload
		if err := common.DecodeJson(r.Body, &payload); err == nil {
			mutex.Lock()
			recipient := strings.TrimPrefix(r.URL.Path, "/")
			received[recipient] = append(received[recipient], payload.Type+" "+payload.Title)
			mutex.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	httpClient, system_setting.WorkerUrl = server.Client(), ""
	fetch.EnableSSRFProtection = false

	on, off := true, false
	for _, user := range []struct {
		name     string
		role     int
		status   int
		receives *bool
		language string
	}{
		{"root-never-chose", common.RoleRootUser, common.UserStatusEnabled, nil, ""},
		{"root-turned-off", common.RoleRootUser, common.UserStatusEnabled, &off, ""},
		{"admin-never-chose", common.RoleAdminUser, common.UserStatusEnabled, nil, ""},
		{"admin-turned-on", common.RoleAdminUser, common.UserStatusEnabled, &on, "zhCN"},
		{"disabled-admin-turned-on", common.RoleAdminUser, common.UserStatusDisabled, &on, ""},
		{"user-with-switch-stored-on", common.RoleCommonUser, common.UserStatusEnabled, &on, ""},
	} {
		setting, err := common.Marshal(dto.UserSetting{
			NotifyType:           dto.NotifyTypeWebhook,
			WebhookUrl:           server.URL + "/" + user.name,
			ChannelNotifyEnabled: user.receives,
			Language:             user.language,
		})
		require.NoError(t, err)
		require.NoError(t, database.Create(&model.User{Username: user.name, Role: user.role, Status: user.status, Setting: string(setting), AffCode: user.name}).Error)
	}
	channel := &model.Channel{Name: "notice-review", Key: "fixture-key", Type: 1, Status: common.ChannelStatusEnabled, Group: "default", Models: "test-model"}
	require.NoError(t, channel.Insert())

	DisableChannel(types.ChannelError{ChannelId: channel.Id, ChannelName: channel.Name, AutoBan: true}, "upstream failed")
	EnableChannel(channel.Id, "", channel.Name)
	NotifyChannelWatchers(dto.NotifyTypeChannelTest, func(lang string) (string, string) {
		return i18n.Translate(lang, i18n.MsgChannelTestCompletedSubject), i18n.Translate(lang, i18n.MsgChannelTestCompletedContent)
	})

	disabledType := formatNotifyType(channel.Id, common.ChannelStatusAutoDisabled)
	enabledType := formatNotifyType(channel.Id, common.ChannelStatusEnabled)
	assert.Equal(t, map[string][]string{
		"root-never-chose": {
			fmt.Sprintf(`%s Channel "notice-review" (#%d) has been disabled`, disabledType, channel.Id),
			fmt.Sprintf(`%s Channel "notice-review" (#%d) has been enabled`, enabledType, channel.Id),
			"channel_test Channel test completed",
		},
		"admin-turned-on": {
			fmt.Sprintf("%s 通道「notice-review」（#%d）已被禁用", disabledType, channel.Id),
			fmt.Sprintf("%s 通道「notice-review」（#%d）已被启用", enabledType, channel.Id),
			"channel_test 通道测试完成",
		},
	}, received)
}

type robotTestTransport struct {
	target *url.URL
	transport http.RoundTripper
}

func (transport robotTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Scheme, request.URL.Host = transport.target.Scheme, transport.target.Host
	return transport.transport.RoundTrip(request)
}

func TestRobotNotificationsSendProviderPayloadAndSignedURL(t *testing.T) {
	previousClient, previousWorker := httpClient, system_setting.WorkerUrl
	fetch := system_setting.GetFetchSetting()
	previousFetch := *fetch
	t.Cleanup(func() {
		httpClient, system_setting.WorkerUrl = previousClient, previousWorker
		*fetch = previousFetch
	})
	fetch.EnableSSRFProtection = false
	system_setting.WorkerUrl = ""
	for _, kind := range []string{dto.NotifyTypeDingTalk, dto.NotifyTypeWeCom} {
		t.Run(kind, func(t *testing.T) {
			var payload map[string]any
			var query url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.NoError(t, common.DecodeJson(r.Body, &payload))
				query = r.URL.Query()
				fmt.Fprint(w, `{"errcode":0,"errmsg":"ok"}`)
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			httpClient = &http.Client{Transport: robotTestTransport{target: target, transport: server.Client().Transport}}
			endpoint := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=robot-key"
			if kind == dto.NotifyTypeDingTalk {
				endpoint = "https://oapi.dingtalk.com/robot/send?access_token=robot-token"
			}
			require.NoError(t, sendRobotNotify(kind, endpoint, "signing-secret", dto.Notify{Title: "Alert", Content: "Remaining " + dto.ContentValueParam, Values: []any{42}}))
			assert.Equal(t, map[string]any{"msgtype": "text", "text": map[string]any{"content": "Alert\nRemaining 42"}}, payload)
			if kind == dto.NotifyTypeDingTalk {
				assert.Equal(t, "robot-token", query.Get("access_token"))
				require.NotEmpty(t, query.Get("timestamp"))
				mac := hmac.New(sha256.New, []byte("signing-secret"))
				mac.Write([]byte(query.Get("timestamp") + "\nsigning-secret"))
				assert.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), query.Get("sign"))
			} else {
				assert.Equal(t, "robot-key", query.Get("key"))
				assert.Empty(t, query.Get("sign"))
			}
		})
	}
}

func TestRobotNotificationsRejectUnsafeURLsAndRedactErrors(t *testing.T) {
	for _, endpoint := range []string{
		"http://oapi.dingtalk.com/robot/send?access_token=secret",
		"https://127.0.0.1/robot/send?access_token=secret",
		"https://oapi.dingtalk.com.evil.example/robot/send?access_token=secret",
		"https://user:secret@oapi.dingtalk.com/robot/send?access_token=secret",
		"https://oapi.dingtalk.com:443/robot/send?access_token=secret",
		"https://oapi.dingtalk.com/robot/send?access_token=secret&access_token=other",
		"https://oapi.dingtalk.com/robot/send",
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret",
	} {
		err := sendRobotNotify(dto.NotifyTypeDingTalk, endpoint, "signing-secret", dto.Notify{})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret")
		assert.NotContains(t, err.Error(), endpoint)
	}
}

func TestRobotNotificationsRejectProviderFailuresWithoutLeakingSecrets(t *testing.T) {
	previousClient, previousWorker := httpClient, system_setting.WorkerUrl
	fetch := system_setting.GetFetchSetting()
	previousFetch := *fetch
	t.Cleanup(func() {
		httpClient, system_setting.WorkerUrl = previousClient, previousWorker
		*fetch = previousFetch
	})
	fetch.EnableSSRFProtection = false
	system_setting.WorkerUrl = ""
	for _, tc := range []struct {
		name string
		status int
		body string
		want string
	}{
		{"provider rejection", 200, `{"errcode":310000,"errmsg":"secret robot-token"}`, "rejected with code: 310000"},
		{"invalid response", 200, `{"errmsg":"secret robot-token"}`, "invalid robot notification response"},
		{"HTTP rejection", 403, "secret robot-token", "status code: 403"},
		{"redirect rejected", 302, "secret robot-token", "status code: 302"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Location", "https://evil.example/secret")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			httpClient = &http.Client{Transport: robotTestTransport{target: target, transport: server.Client().Transport}}
			err = sendRobotNotify(dto.NotifyTypeDingTalk, "https://oapi.dingtalk.com/robot/send?access_token=robot-token", "secret", dto.Notify{})
			require.ErrorContains(t, err, tc.want)
			assert.NotContains(t, err.Error(), "secret")
			assert.NotContains(t, err.Error(), "robot-token")
			assert.Equal(t, 1, requests)
		})
	}
}
