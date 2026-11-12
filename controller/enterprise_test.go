package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct {
		name, env string
		kind      common.DatabaseType
	}{{"sqlite", "", common.DatabaseTypeSQLite}, {"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL}, {"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL}} {
		t.Run(dialect.name, func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dialect.env != "" && dsn == "" {
				t.Skip("database DSN not configured")
			}
			db, _ := newAuditTestDatabase(t, dialect.name, dsn)
			oldDB, oldLog := model.DB, model.LOG_DB
			oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
			t.Cleanup(func() {
				model.DB = oldDB
				model.LOG_DB = oldLog
				common.SetDatabaseTypes(oldMain, oldLogType)
				common.RedisEnabled = oldRedis
				common.BatchUpdateEnabled = oldBatch
			})
			model.DB = db
			model.LOG_DB = db
			common.SetDatabaseTypes(dialect.kind, dialect.kind)
			common.RedisEnabled = false
			common.BatchUpdateEnabled = false
			require.NoError(t, db.AutoMigrate(&model.User{}, &legacyToken{}))
			users := []model.User{{Username: "enterprise-root", Role: common.RoleRootUser, Status: 1, Quota: 500, AffCode: "er"}, {Username: "enterprise-owner", Role: 1, Status: 1, Quota: 500, AffCode: "eo"}, {Username: "enterprise-member", Role: 1, Status: 1, Quota: 500, AffCode: "em"}, {Username: "enterprise-outsider", Role: 1, Status: 1, Quota: 500, AffCode: "ex"}}
			require.NoError(t, db.Create(&users).Error)
			personal := legacyToken{UserId: users[2].Id, Key: "personal-existing", Name: "personal", Status: 1, ExpiredTime: -1, UnlimitedQuota: true}
			require.NoError(t, db.Create(&personal).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.Enterprise{}, &model.EnterpriseMember{}, &model.EnterpriseAudit{}, &model.EnterpriseCharge{}, &model.EnterpriseBalanceEntry{}, &model.Token{}, &model.User{}, &model.TrustedDevice{}, &model.PasswordLoginGuard{}))
			}
			var version string
			query := "SELECT version()"
			if dialect.name == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Log("database version:", version)
			enterprise, err := model.CreateEnterprise(users[0].Id, users[1].Id, "Accounting team")
			require.NoError(t, err)
			require.NoError(t, model.AddEnterpriseMember(users[1].Id, enterprise.Id, users[2].Username, "member", 300))
			require.NoError(t, model.AdjustEnterpriseBalance(users[0].Id, enterprise.Id, 1000, "Initial credit"))
			require.ErrorIs(t, model.ValidateEnterpriseAccess(enterprise.Id, users[3].Id), model.ErrEnterpriseAccess)
			require.ErrorIs(t, model.UpdateEnterpriseMember(users[2].Id, enterprise.Id, users[1].Id, "admin", "active", 0), model.ErrEnterpriseAccess)
			require.ErrorIs(t, model.AdjustEnterpriseBalance(users[1].Id, enterprise.Id, 100, "Not allowed"), model.ErrEnterpriseAccess)
			require.NoError(t, model.ReserveEnterpriseQuota(enterprise.Id, users[2].Id, 10, "request-a", 200))
			require.NoError(t, model.ReserveEnterpriseQuota(enterprise.Id, users[2].Id, 10, "request-a", 200))
			require.ErrorIs(t, model.ReserveEnterpriseQuota(enterprise.Id, users[2].Id, 10, "request-b", 101), model.ErrEnterpriseQuota)
			require.NoError(t, model.SettleEnterpriseQuota("request-a", 250))
			require.NoError(t, model.SettleEnterpriseQuota("request-a", 250))
			var actual model.Enterprise
			require.NoError(t, db.First(&actual, enterprise.Id).Error)
			assert.EqualValues(t, 750, actual.Balance)
			assert.EqualValues(t, 250, actual.UsedQuota)
			require.NoError(t, model.UpdateEnterpriseMember(users[1].Id, enterprise.Id, users[2].Id, "member", "removed", 300))
			require.ErrorIs(t, model.ValidateEnterpriseAccess(enterprise.Id, users[2].Id), model.ErrEnterpriseAccess)
			require.NoError(t, model.RefundEnterpriseQuota("request-a"))
			require.NoError(t, model.RefundEnterpriseQuota("request-a"))
			require.NoError(t, db.First(&actual, enterprise.Id).Error)
			assert.EqualValues(t, 1000, actual.Balance)
			assert.Zero(t, actual.UsedQuota)
			require.Error(t, model.SettleEnterpriseQuota("request-a", 50))
			require.NoError(t, model.UpdateEnterpriseMember(users[1].Id, enterprise.Id, users[2].Id, "member", "active", 300))
			companyKey := model.Token{EnterpriseId: enterprise.Id, UserId: users[2].Id, Key: "enterprise-key", Name: "company", Status: 1, ExpiredTime: -1, UnlimitedQuota: true}
			require.NoError(t, db.Create(&companyKey).Error)
			tokens, err := model.GetAllUserTokens(users[2].Id, 0, 20)
			require.NoError(t, err)
			require.Len(t, tokens, 1)
			assert.Equal(t, personal.Id, tokens[0].Id)
			_, err = model.GetTokenByIds(companyKey.Id, users[2].Id)
			require.Error(t, err)
			count, err := model.BatchDeleteTokens([]int{companyKey.Id}, users[2].Id)
			require.NoError(t, err)
			assert.Zero(t, count)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{EnterpriseId: enterprise.Id, UserId: users[2].Id, TokenId: companyKey.Id, TokenKey: companyKey.Key, TokenUnlimited: true, RequestId: "gateway-request"}
			session, apiErr := service.NewBillingSession(c, info, 100)
			require.Nil(t, apiErr)
			require.NoError(t, session.Reserve(120))
			require.NoError(t, session.Settle(150))
			require.NoError(t, session.Settle(150))
			require.NoError(t, db.First(&actual, enterprise.Id).Error)
			assert.EqualValues(t, 850, actual.Balance)
			var memberUser model.User
			require.NoError(t, db.First(&memberUser, users[2].Id).Error)
			assert.Equal(t, 500, memberUser.Quota, "enterprise charges never debit personal balance")
			managed, err := model.CreateEnterpriseManagedAccount(users[1].Id, enterprise.Id, "managed-worker", "Worker", "managed-password-long-123", 100)
			require.NoError(t, err)
			isManaged, err := model.IsManagedEnterpriseUser(managed.Id)
			require.NoError(t, err)
			assert.True(t, isManaged)
			assert.Equal(t, enterprise.Id, managed.ManagedEnterpriseId)
			require.NoError(t, db.First(&memberUser, users[2].Id).Error)
			assert.Zero(t, memberUser.ManagedEnterpriseId)
			require.Error(t, model.AddEnterpriseMember(users[1].Id, enterprise.Id, managed.Username, "admin", 0))
			require.Error(t, model.TransferEnterpriseOwner(users[1].Id, enterprise.Id, managed.Id))
			require.NoError(t, model.TransferEnterpriseOwner(users[1].Id, enterprise.Id, users[2].Id))
			var owners int64
			require.NoError(t, db.Model(&model.EnterpriseMember{}).Where("enterprise_id = ? AND role = ?", enterprise.Id, "owner").Count(&owners).Error)
			assert.EqualValues(t, 1, owners)
			// Tenant-scoped list authorization must not leak keys or balances to outsiders.
			engine := gin.New()
			engine.Use(func(c *gin.Context) { c.Set("id", users[3].Id); c.Set("role", 1) })
			engine.GET("/api/enterprises/:enterprise/keys", ListEnterpriseKeys)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/enterprises/%d/keys", enterprise.Id), nil))
			assert.Equal(t, http.StatusForbidden, recorder.Code)
			assert.NotContains(t, recorder.Body.String(), companyKey.Key)
			// The same request identifier cannot debit a different enterprise or member.
			second, err := model.CreateEnterprise(users[0].Id, users[1].Id, "Other tenant")
			require.NoError(t, err)
			require.NoError(t, model.AdjustEnterpriseBalance(users[0].Id, second.Id, 500, "Credit"))
			require.Error(t, model.ReserveEnterpriseQuota(second.Id, users[1].Id, 10, "gateway-request", 50))
			// Actual upstream cost may exceed admission; debt is recorded, never a personal debit.
			require.NoError(t, model.ReserveEnterpriseQuota(second.Id, users[1].Id, 10, "debt-request", 500))
			require.NoError(t, model.SettleEnterpriseQuota("debt-request", 600))
			available, err := model.GetEnterpriseAvailableQuota(second.Id, users[1].Id)
			require.NoError(t, err)
			assert.Zero(t, available)
			require.ErrorIs(t, model.ReserveEnterpriseQuota(second.Id, users[1].Id, 10, "blocked-request", 1), model.ErrEnterpriseQuota)
			require.NoError(t, model.UpdateEnterprise(users[0].Id, second.Id, second.Name, "suspended"))
			require.NoError(t, model.RefundEnterpriseQuota("debt-request"))
			// Device trust bypasses only account lockout, never credential verification.
			require.NoError(t, db.AutoMigrate(&model.UserSession{}))
			identity := model.AuthSessionIdentity{UserID: users[3].Id, SessionID: "trusted-device-test", UserAuthVersion: users[3].AuthVersion, SessionVersion: 1}
			require.NoError(t, db.Create(&model.UserSession{SID: identity.SessionID, UserID: identity.UserID, Version: 1, UserAuthVersion: identity.UserAuthVersion, Status: model.UserSessionStatusActive, ExpiresAt: common.GetTimestamp() + 3600}).Error)
			deviceSecret, err := model.EnrollTrustedDevice(identity, "Test browser", "Regression browser")
			require.NoError(t, err)
			for range 10 {
				require.ErrorIs(t, model.CheckPasswordLoginGuard(&users[3], false, ""), model.ErrInvalidCredentials)
			}
			require.ErrorIs(t, model.CheckPasswordLoginGuard(&users[3], true, ""), model.ErrInvalidCredentials)
			require.ErrorIs(t, model.CheckPasswordLoginGuard(&users[3], false, deviceSecret), model.ErrInvalidCredentials)
			require.NoError(t, model.CheckPasswordLoginGuard(&users[3], true, deviceSecret))
			for range 4 {
				_ = model.CheckPasswordLoginGuard(&users[3], false, deviceSecret)
			}
			require.ErrorIs(t, model.CheckPasswordLoginGuard(&users[3], true, deviceSecret), model.ErrInvalidCredentials, "trusted device attempts remain limited")
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", users[3].Id).Update("auth_version", 2).Error)
			users[3].AuthVersion = 2
			for range 10 {
				_ = model.CheckPasswordLoginGuard(&users[3], false, "")
			}
			require.ErrorIs(t, model.CheckPasswordLoginGuard(&users[3], true, deviceSecret), model.ErrInvalidCredentials, "security changes invalidate device credentials")
		})
	}
}
