package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func enterpriseFailure(c *gin.Context, err error) {
	if err == model.ErrEnterpriseAccess {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Enterprise access denied"))
		return
	}
	if err == model.ErrEnterpriseInput || err == model.ErrEnterpriseQuota {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage(err.Error()))
		return
	}
	_ = c.Error(err)
	common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Please try again later."))
}
func enterpriseID(c *gin.Context) int     { id, _ := strconv.Atoi(c.Param("enterprise")); return id }
func enterpriseTarget(c *gin.Context) int { id, _ := strconv.Atoi(c.Param("target")); return id }
func enterpriseRole(c *gin.Context) (string, bool) {
	role, err := model.EnterpriseRole(enterpriseID(c), c.GetInt("id"))
	if err != nil {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Enterprise access denied"))
		return "", false
	}
	return role, true
}
func enterpriseCanManage(role string) bool {
	return role == "owner" || role == "admin" || role == "site_admin"
}
func enterpriseProof(c *gin.Context, action string, target int) bool {
	payload, err := common.Marshal(gin.H{"enterprise_id": enterpriseID(c), "action": action, "target_id": target})
	if err != nil {
		enterpriseFailure(c, err)
		return false
	}
	return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeEnterprise, Context: payload}) != nil
}

func ListEnterprises(c *gin.Context) {
	var enterprises []model.Enterprise
	siteAdmin := c.GetInt("role") >= common.RoleAdminUser
	query := model.DB.Model(&model.Enterprise{})
	if !siteAdmin {
		query = query.Where("id IN (?)", model.DB.Model(&model.EnterpriseMember{}).Select("enterprise_id").Where("user_id = ? AND status = ?", c.GetInt("id"), "active"))
	}
	if err := query.Order("id desc").Find(&enterprises).Error; err != nil {
		enterpriseFailure(c, err)
		return
	}
	if !siteAdmin {
		var members []model.EnterpriseMember
		if err := model.DB.Where("user_id = ? AND status = ?", c.GetInt("id"), "active").Find(&members).Error; err != nil {
			enterpriseFailure(c, err)
			return
		}
		roles := make(map[int]string, len(members))
		for _, member := range members {
			roles[member.EnterpriseId] = member.Role
		}
		for i := range enterprises {
			if roles[enterprises[i].Id] == "member" {
				enterprises[i].Balance = 0
				enterprises[i].UsedQuota = 0
			}
		}
	}
	common.ApiSuccess(c, gin.H{"items": enterprises, "site_admin": siteAdmin})
}

func GetEnterprise(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	var enterprise model.Enterprise
	if err := model.DB.First(&enterprise, enterpriseID(c)).Error; err != nil {
		enterpriseFailure(c, err)
		return
	}
	if role == "member" {
		enterprise.Balance = 0
		enterprise.UsedQuota = 0
	}
	common.ApiSuccess(c, gin.H{"enterprise": enterprise, "role": role})
}

func CreateEnterprise(c *gin.Context) {
	if c.GetInt("role") < common.RoleAdminUser {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Enterprise access denied"))
		return
	}
	var input struct {
		Name    string `json:"name"`
		OwnerId int    `json:"owner_id"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	if !enterpriseProof(c, "create", input.OwnerId) {
		return
	}
	enterprise, err := model.CreateEnterprise(c.GetInt("id"), input.OwnerId, input.Name)
	if err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, enterprise)
}

func UpdateEnterprise(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if !enterpriseCanManage(role) {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var input struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	if !enterpriseProof(c, "update", 0) {
		return
	}
	if err := model.UpdateEnterprise(c.GetInt("id"), enterpriseID(c), input.Name, input.Status); err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func ListEnterpriseMembers(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	type memberView struct {
		model.EnterpriseMember
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	var items []memberView
	query := model.DB.Table("enterprise_members").Select("enterprise_members.*, users.username, users.display_name").Joins("JOIN users ON users.id = enterprise_members.user_id").Where("enterprise_members.enterprise_id = ?", enterpriseID(c))
	if role == "member" {
		query = query.Where("enterprise_members.user_id = ?", c.GetInt("id"))
	}
	enterprisePage(c, query, "enterprise_members.id desc", &items)
}

func AddEnterpriseMember(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if !enterpriseCanManage(role) {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var input struct {
		Username   string `json:"username"`
		Role       string `json:"role"`
		QuotaLimit int64  `json:"quota_limit"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	if !enterpriseProof(c, "member.add", 0) {
		return
	}
	if err := model.AddEnterpriseMember(c.GetInt("id"), enterpriseID(c), input.Username, input.Role, input.QuotaLimit); err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func UpdateEnterpriseMember(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if !enterpriseCanManage(role) {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var input struct {
		Role       string `json:"role"`
		Status     string `json:"status"`
		QuotaLimit int64  `json:"quota_limit"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	if !enterpriseProof(c, "member.update", enterpriseTarget(c)) {
		return
	}
	if err := model.UpdateEnterpriseMember(c.GetInt("id"), enterpriseID(c), enterpriseTarget(c), input.Role, input.Status, input.QuotaLimit); err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func CreateEnterpriseManagedAccount(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if !enterpriseCanManage(role) {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var input struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		QuotaLimit  int64  `json:"quota_limit"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	if !enterpriseProof(c, "managed.create", 0) {
		return
	}
	user, err := model.CreateEnterpriseManagedAccount(c.GetInt("id"), enterpriseID(c), input.Username, input.DisplayName, input.Password, input.QuotaLimit)
	if err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": user.Id, "username": user.Username})
}

func TransferEnterpriseOwner(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if role != "owner" && role != "site_admin" {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var input struct {
		UserId int `json:"user_id"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	if !enterpriseProof(c, "owner.transfer", input.UserId) {
		return
	}
	if err := model.TransferEnterpriseOwner(c.GetInt("id"), enterpriseID(c), input.UserId); err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdjustEnterpriseBalance(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if role != "site_admin" {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var input struct {
		Amount int64  `json:"amount"`
		Reason string `json:"reason"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	if !enterpriseProof(c, "balance.adjust", 0) {
		return
	}
	if err := model.AdjustEnterpriseBalance(c.GetInt("id"), enterpriseID(c), input.Amount, input.Reason); err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func enterprisePage(c *gin.Context, query *gorm.DB, order string, items any) {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("page_size"))
	page = max(1, min(page, 100000))
	if size <= 0 {
		size = 20
	}
	size = min(size, 100)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		enterpriseFailure(c, err)
		return
	}
	if err := query.Order(order).Offset((page - 1) * size).Limit(size).Find(items).Error; err != nil {
		enterpriseFailure(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func ListEnterpriseLedger(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	var items []model.EnterpriseCharge
	query := model.DB.Model(&model.EnterpriseCharge{}).Where("enterprise_id = ?", enterpriseID(c))
	if role == "member" {
		query = query.Where("user_id = ?", c.GetInt("id"))
	}
	enterprisePage(c, query, "id desc", &items)
}
func ListEnterpriseAudit(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if role == "member" {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var items []model.EnterpriseAudit
	query := model.DB.Model(&model.EnterpriseAudit{}).Where("enterprise_id = ?", enterpriseID(c))
	if role == "billing" {
		query = query.Where("action = ?", "balance.adjust")
	}
	enterprisePage(c, query, "id desc", &items)
}
func ListEnterpriseBalanceEntries(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if role == "member" {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var items []model.EnterpriseBalanceEntry
	enterprisePage(c, model.DB.Model(&model.EnterpriseBalanceEntry{}).Where("enterprise_id = ?", enterpriseID(c)), "id desc", &items)
}
func ListEnterpriseKeys(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if role == "billing" {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var items []model.Token
	query := model.DB.Model(&model.Token{}).Select("id,user_id,enterprise_id,name,status,created_time,expired_time,remain_quota,used_quota,unlimited_quota,model_limits_enabled,model_limits,allow_ips").Where("enterprise_id = ?", enterpriseID(c))
	if role == "member" {
		query = query.Where("user_id = ?", c.GetInt("id"))
	}
	enterprisePage(c, query, "id desc", &items)
}

type enterpriseKeyInput struct {
	UserId         int    `json:"user_id"`
	Name           string `json:"name"`
	Status         int    `json:"status"`
	ExpiredTime    int64  `json:"expired_time"`
	RemainQuota    int    `json:"remain_quota"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
	ModelLimits    string `json:"model_limits"`
	AllowIps       string `json:"allow_ips"`
}

func MutateEnterpriseKey(c *gin.Context) {
	role, ok := enterpriseRole(c)
	if !ok {
		return
	}
	if role == "billing" {
		enterpriseFailure(c, model.ErrEnterpriseAccess)
		return
	}
	var input enterpriseKeyInput
	deleting := c.Request.Method == http.MethodDelete
	if !deleting && common.DecodeJson(c.Request.Body, &input) != nil {
		enterpriseFailure(c, model.ErrEnterpriseInput)
		return
	}
	target := enterpriseTarget(c)
	action := "key.create"
	if target > 0 {
		action = "key.update"
	}
	if deleting {
		action = "key.delete"
	}
	if !enterpriseProof(c, action, target) {
		return
	}
	var result model.Token
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		role, err = model.LockEnterpriseManagement(tx, enterpriseID(c), c.GetInt("id"))
		if err != nil {
			return err
		}
		if role == "billing" {
			return model.ErrEnterpriseAccess
		}
		var enterprise model.Enterprise
		if err := tx.Where("id = ? AND status = ?", enterpriseID(c), "active").First(&enterprise).Error; err != nil {
			return model.ErrEnterpriseAccess
		}
		if target > 0 {
			if err := tx.Where("id = ? AND enterprise_id = ?", target, enterprise.Id).First(&result).Error; err != nil {
				return model.ErrEnterpriseAccess
			}
			if role == "member" && result.UserId != c.GetInt("id") {
				return model.ErrEnterpriseAccess
			}
		} else {
			if input.UserId == 0 {
				input.UserId = c.GetInt("id")
			}
			if role == "member" && input.UserId != c.GetInt("id") {
				return model.ErrEnterpriseAccess
			}
			result.UserId = input.UserId
			result.EnterpriseId = enterprise.Id
		}
		if !deleting {
			if len(strings.TrimSpace(input.Name)) == 0 || len(input.Name) > 50 || input.RemainQuota < 0 || input.RemainQuota > maxTokenQuota() || (input.ExpiredTime != -1 && input.ExpiredTime <= common.GetTimestamp()) || len(input.ModelLimits) > 8192 || len(input.AllowIps) > 4096 {
				return model.ErrEnterpriseInput
			}
			var member model.EnterpriseMember
			if err := tx.Where("enterprise_id = ? AND user_id = ? AND status = ?", enterprise.Id, result.UserId, "active").First(&member).Error; err != nil {
				return model.ErrEnterpriseAccess
			}
			var user model.User
			if err := tx.Select("id", "status").First(&user, result.UserId).Error; err != nil || user.Status != common.UserStatusEnabled {
				return model.ErrEnterpriseAccess
			}
			result.Name = input.Name
			result.Status = common.TokenStatusEnabled
			if input.Status == common.TokenStatusDisabled {
				result.Status = input.Status
			}
			result.ExpiredTime = input.ExpiredTime
			result.RemainQuota = input.RemainQuota
			result.UnlimitedQuota = input.UnlimitedQuota
			result.ModelLimits = input.ModelLimits
			result.ModelLimitsEnabled = input.ModelLimits != ""
			result.AllowIps = &input.AllowIps
			if target == 0 {
				key, err := common.GenerateKey()
				if err != nil {
					return err
				}
				result.Key = key
				result.CreatedTime = common.GetTimestamp()
			}
		}
		if err := model.InvalidateUserTokensCache(result.UserId); err != nil {
			return err
		}
		if deleting {
			if err := tx.Delete(&result).Error; err != nil {
				return err
			}
		} else if target == 0 {
			if err := tx.Create(&result).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&result).Select("name", "status", "expired_time", "remain_quota", "unlimited_quota", "model_limits", "model_limits_enabled", "allow_ips").Updates(&result).Error; err != nil {
				return err
			}
		}
		return tx.Create(&model.EnterpriseAudit{EnterpriseId: enterprise.Id, ActorId: c.GetInt("id"), TargetId: result.Id, Action: action, CreatedAt: common.GetTimestamp()}).Error
	})
	if err != nil {
		enterpriseFailure(c, err)
		return
	}
	if action == "key.create" {
		common.ApiSuccess(c, gin.H{"id": result.Id, "key": "sk-" + result.Key})
		return
	}
	common.ApiSuccess(c, nil)
}
