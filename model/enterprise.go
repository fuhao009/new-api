package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrEnterpriseAccess = errors.New("Enterprise access denied")
var ErrEnterpriseInput = errors.New("Invalid enterprise operation")

// Enterprise roles are tenant-local, never platform administrator roles.
type Enterprise struct {
	Id        int    `json:"id"`
	Name      string `json:"name" gorm:"size:128;not null"`
	Status    string `json:"status" gorm:"size:16;not null"`
	OwnerId   int    `json:"owner_id" gorm:"index"`
	Balance   int64  `json:"balance" gorm:"not null;default:0"`
	UsedQuota int64  `json:"used_quota" gorm:"not null;default:0"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type EnterpriseMember struct {
	Id           int    `json:"id"`
	EnterpriseId int    `json:"enterprise_id" gorm:"uniqueIndex:idx_enterprise_member,priority:1;index"`
	UserId       int    `json:"user_id" gorm:"uniqueIndex:idx_enterprise_member,priority:2;index"`
	Role         string `json:"role" gorm:"size:16;not null"`
	Status       string `json:"status" gorm:"size:16;not null"`
	Managed      bool   `json:"managed"`
	QuotaLimit   int64  `json:"quota_limit"`
	UsedQuota    int64  `json:"used_quota"`
	CreatedAt    int64  `json:"created_at"`
}

type EnterpriseAudit struct {
	Id           int    `json:"id"`
	EnterpriseId int    `json:"enterprise_id" gorm:"index"`
	ActorId      int    `json:"actor_id"`
	TargetId     int    `json:"target_id"`
	Action       string `json:"action" gorm:"size:64"`
	Detail       string `json:"detail" gorm:"type:text"`
	CreatedAt    int64  `json:"created_at" gorm:"index"`
}

func enterpriseAudit(tx *gorm.DB, enterpriseId, actorId, targetId int, action, detail string) error {
	return tx.Create(&EnterpriseAudit{EnterpriseId: enterpriseId, ActorId: actorId, TargetId: targetId, Action: action, Detail: detail, CreatedAt: common.GetTimestamp()}).Error
}

func enterpriseAccess(tx *gorm.DB, enterpriseId, userId int, management bool) (*Enterprise, *EnterpriseMember, error) {
	var enterprise Enterprise
	if err := lockForUpdate(tx).First(&enterprise, enterpriseId).Error; err != nil {
		return nil, nil, ErrEnterpriseAccess
	}
	var user User
	if err := tx.Select("id", "role", "status").First(&user, userId).Error; err != nil || user.Status != common.UserStatusEnabled {
		return nil, nil, ErrEnterpriseAccess
	}
	if management && user.Role >= common.RoleAdminUser {
		return &enterprise, &EnterpriseMember{UserId: userId, Role: "site_admin", Status: "active"}, nil
	}
	var member EnterpriseMember
	if enterprise.Status != "active" {
		return nil, nil, ErrEnterpriseAccess
	}
	if err := lockForUpdate(tx).Where("enterprise_id = ? AND user_id = ?", enterpriseId, userId).First(&member).Error; err != nil || member.Status != "active" {
		return nil, nil, ErrEnterpriseAccess
	}
	return &enterprise, &member, nil
}

func ValidateEnterpriseAccess(enterpriseId, userId int) error {
	return DB.Transaction(func(tx *gorm.DB) error { _, _, err := enterpriseAccess(tx, enterpriseId, userId, false); return err })
}

func IsManagedEnterpriseUser(userId int) (bool, error) {
	var count int64
	err := DB.Model(&EnterpriseMember{}).Where("user_id = ? AND managed = ?", userId, true).Count(&count).Error
	return count > 0, err
}

func EnterpriseRole(enterpriseId, userId int) (string, error) {
	var role string
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, member, err := enterpriseAccess(tx, enterpriseId, userId, true)
		if err == nil {
			role = member.Role
		}
		return err
	})
	return role, err
}

func enterpriseManager(role string) bool {
	return role == "site_admin" || role == "owner" || role == "admin"
}

func CreateEnterprise(actorId, ownerId int, name string) (*Enterprise, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || ownerId <= 0 {
		return nil, ErrEnterpriseInput
	}
	enterprise := Enterprise{Name: name, OwnerId: ownerId, Status: "active", CreatedAt: common.GetTimestamp(), UpdatedAt: common.GetTimestamp()}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var actor, owner User
		if err := tx.First(&actor, actorId).Error; err != nil || actor.Role < common.RoleAdminUser || actor.Status != common.UserStatusEnabled {
			return ErrEnterpriseAccess
		}
		if err := lockForUpdate(tx).First(&owner, ownerId).Error; err != nil || owner.Status != common.UserStatusEnabled {
			return ErrEnterpriseInput
		}
		var managed int64
		if err := tx.Model(&EnterpriseMember{}).Where("user_id = ? AND managed = ?", ownerId, true).Count(&managed).Error; err != nil {
			return err
		}
		if managed > 0 {
			return ErrEnterpriseInput
		}
		if err := tx.Create(&enterprise).Error; err != nil {
			return err
		}
		if err := tx.Create(&EnterpriseMember{EnterpriseId: enterprise.Id, UserId: ownerId, Role: "owner", Status: "active", CreatedAt: common.GetTimestamp()}).Error; err != nil {
			return err
		}
		return enterpriseAudit(tx, enterprise.Id, actorId, ownerId, "enterprise.create", name)
	})
	return &enterprise, err
}

func UpdateEnterprise(actorId, enterpriseId int, name, status string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || (status != "active" && status != "suspended") {
		return ErrEnterpriseInput
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		enterprise, member, err := enterpriseAccess(tx, enterpriseId, actorId, true)
		if err != nil {
			return err
		}
		if !enterpriseManager(member.Role) || (status != enterprise.Status && member.Role != "site_admin") {
			return ErrEnterpriseAccess
		}
		if err := tx.Model(enterprise).Updates(map[string]any{"name": name, "status": status, "updated_at": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		return enterpriseAudit(tx, enterpriseId, actorId, 0, "enterprise.update", name+":"+status)
	})
}

// UpdateEnterpriseMember preserves historical membership for settlement and audit.
func UpdateEnterpriseMember(actorId, enterpriseId, userId int, role, status string, limit int64) error {
	if (role != "admin" && role != "billing" && role != "member") || (status != "active" && status != "suspended" && status != "removed") || limit < 0 || limit > common.MaxWalletQuota {
		return ErrEnterpriseInput
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		_, actor, err := enterpriseAccess(tx, enterpriseId, actorId, true)
		if err != nil {
			return err
		}
		if !enterpriseManager(actor.Role) {
			return ErrEnterpriseAccess
		}
		var member EnterpriseMember
		if err := lockForUpdate(tx).Where("enterprise_id = ? AND user_id = ?", enterpriseId, userId).First(&member).Error; err != nil {
			return ErrEnterpriseAccess
		}
		if member.Role == "owner" || userId == actorId || (actor.Role == "admin" && (member.Role != "member" || role != "member")) {
			return ErrEnterpriseAccess
		}
		if err := tx.Model(&member).Updates(map[string]any{"role": role, "status": status, "quota_limit": limit}).Error; err != nil {
			return err
		}
		return enterpriseAudit(tx, enterpriseId, actorId, userId, "member.update", role+":"+status)
	})
}

func AddEnterpriseMember(actorId, enterpriseId int, username, role string, limit int64) error {
	if (role != "admin" && role != "billing" && role != "member") || limit < 0 || limit > common.MaxWalletQuota {
		return ErrEnterpriseInput
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		_, actor, err := enterpriseAccess(tx, enterpriseId, actorId, true)
		if err != nil {
			return err
		}
		if !enterpriseManager(actor.Role) || (actor.Role == "admin" && role != "member") {
			return ErrEnterpriseAccess
		}
		var user User
		if err := lockForUpdate(tx).Where("username = ? AND status = ?", strings.TrimSpace(username), common.UserStatusEnabled).First(&user).Error; err != nil {
			return ErrEnterpriseInput
		}
		var managed int64
		if err := tx.Model(&EnterpriseMember{}).Where("user_id = ? AND managed = ?", user.Id, true).Count(&managed).Error; err != nil {
			return err
		}
		if managed > 0 {
			return ErrEnterpriseInput
		}
		member := EnterpriseMember{EnterpriseId: enterpriseId, UserId: user.Id, Role: role, Status: "active", QuotaLimit: limit, CreatedAt: common.GetTimestamp()}
		if err := tx.Create(&member).Error; err != nil {
			return ErrEnterpriseInput
		}
		return enterpriseAudit(tx, enterpriseId, actorId, user.Id, "member.add", role)
	})
}

func TransferEnterpriseOwner(actorId, enterpriseId, userId int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		enterprise, actor, err := enterpriseAccess(tx, enterpriseId, actorId, true)
		if err != nil {
			return err
		}
		if actor.Role != "owner" && actor.Role != "site_admin" {
			return ErrEnterpriseAccess
		}
		if userId == enterprise.OwnerId {
			return ErrEnterpriseInput
		}
		var target EnterpriseMember
		if err := lockForUpdate(tx).Where("enterprise_id = ? AND user_id = ? AND status = ?", enterpriseId, userId, "active").First(&target).Error; err != nil || target.Managed {
			return ErrEnterpriseInput
		}
		if err := tx.Model(&EnterpriseMember{}).Where("enterprise_id = ? AND user_id = ?", enterpriseId, enterprise.OwnerId).Update("role", "admin").Error; err != nil {
			return err
		}
		if err := tx.Model(&target).Update("role", "owner").Error; err != nil {
			return err
		}
		if err := tx.Model(enterprise).Update("owner_id", userId).Error; err != nil {
			return err
		}
		return enterpriseAudit(tx, enterpriseId, actorId, userId, "owner.transfer", "")
	})
}

func CreateEnterpriseManagedAccount(actorId, enterpriseId int, username, displayName, password string, limit int64) (*User, error) {
	if len(username) < 1 || len(username) > 20 || len(displayName) > 20 || len([]rune(password)) < 15 || limit < 0 || limit > common.MaxWalletQuota {
		return nil, ErrEnterpriseInput
	}
	if err := common.ValidateNewAccountPassword(password); err != nil {
		return nil, err
	}
	user := User{Username: username, DisplayName: displayName, Password: password, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, ManagedEnterpriseId: enterpriseId}
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, actor, err := enterpriseAccess(tx, enterpriseId, actorId, true)
		if err != nil {
			return err
		}
		if !enterpriseManager(actor.Role) {
			return ErrEnterpriseAccess
		}
		if err := user.InsertWithTx(tx, 0); err != nil {
			return ErrEnterpriseInput
		}
		if err := tx.Model(&user).Update("quota", 0).Error; err != nil {
			return err
		}
		if err := tx.Create(&EnterpriseMember{EnterpriseId: enterpriseId, UserId: user.Id, Role: "member", Status: "active", Managed: true, QuotaLimit: limit, CreatedAt: common.GetTimestamp()}).Error; err != nil {
			return err
		}
		return enterpriseAudit(tx, enterpriseId, actorId, user.Id, "managed_account.create", username)
	})
	user.Password = ""
	user.Quota = 0
	return &user, err
}

// LockEnterpriseManagement serializes credential changes with tenant revocations.
func LockEnterpriseManagement(tx *gorm.DB, enterpriseId, userId int) (string, error) {
	_, member, err := enterpriseAccess(tx, enterpriseId, userId, true)
	if err != nil {
		return "", err
	}
	return member.Role, nil
}
