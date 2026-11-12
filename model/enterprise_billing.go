package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrEnterpriseQuota = errors.New("Enterprise balance or member quota is insufficient")

type EnterpriseCharge struct {
	Id           int    `json:"id"`
	RequestId    string `json:"request_id" gorm:"size:128;uniqueIndex"`
	EnterpriseId int    `json:"enterprise_id" gorm:"index"`
	UserId       int    `json:"user_id" gorm:"index"`
	TokenId      int    `json:"token_id"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status" gorm:"size:16"`
	CreatedAt    int64  `json:"created_at" gorm:"index"`
	UpdatedAt    int64  `json:"updated_at"`
}

type EnterpriseBalanceEntry struct {
	Id           int    `json:"id"`
	EnterpriseId int    `json:"enterprise_id" gorm:"index"`
	ActorId      int    `json:"actor_id"`
	Amount       int64  `json:"amount"`
	Balance      int64  `json:"balance"`
	Reason       string `json:"reason" gorm:"size:256"`
	CreatedAt    int64  `json:"created_at"`
}

func applyEnterpriseCharge(tx *gorm.DB, e *Enterprise, m *EnterpriseMember, charge *EnterpriseCharge, amount int64, admission bool) error {
	if amount < 0 || amount > common.MaxWalletQuota {
		return ErrEnterpriseInput
	}
	delta := amount - charge.Amount
	if admission && delta > 0 && (e.Balance < delta || (m.QuotaLimit > 0 && (m.UsedQuota > m.QuotaLimit || delta > m.QuotaLimit-m.UsedQuota))) {
		return ErrEnterpriseQuota
	}
	if (delta > 0 && (e.UsedQuota > common.MaxWalletQuota-delta || m.UsedQuota > common.MaxWalletQuota-delta || e.Balance < -common.MaxWalletQuota+delta)) || (delta < 0 && e.Balance > common.MaxWalletQuota+delta) {
		return ErrEnterpriseInput
	}
	if e.UsedQuota+delta < 0 || m.UsedQuota+delta < 0 {
		return ErrEnterpriseInput
	}
	if err := tx.Model(e).Updates(map[string]any{"balance": e.Balance - delta, "used_quota": e.UsedQuota + delta}).Error; err != nil {
		return err
	}
	if err := tx.Model(m).Update("used_quota", m.UsedQuota+delta).Error; err != nil {
		return err
	}
	charge.Amount = amount
	charge.UpdatedAt = common.GetTimestamp()
	return tx.Save(charge).Error
}

func ReserveEnterpriseQuota(enterpriseId, userId, tokenId int, requestId string, amount int) error {
	if requestId == "" || len(requestId) > 128 || amount < 0 {
		return ErrEnterpriseInput
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		e, m, err := enterpriseAccess(tx, enterpriseId, userId, false)
		if err != nil {
			return err
		}
		var charge EnterpriseCharge
		err = tx.Where("request_id = ?", requestId).First(&charge).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			charge = EnterpriseCharge{RequestId: requestId, EnterpriseId: enterpriseId, UserId: userId, TokenId: tokenId, Status: "reserved", CreatedAt: common.GetTimestamp()}
		} else if err != nil {
			return err
		}
		if charge.EnterpriseId != enterpriseId || charge.UserId != userId || charge.TokenId != tokenId || charge.Status != "reserved" {
			return ErrEnterpriseInput
		}
		return applyEnterpriseCharge(tx, e, m, &charge, int64(amount), true)
	})
}

func changeEnterpriseCharge(requestId string, amount int, refund bool) error {
	if requestId == "" || amount < 0 {
		return ErrEnterpriseInput
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var snapshot EnterpriseCharge
		if err := tx.Where("request_id = ?", requestId).First(&snapshot).Error; err != nil {
			return err
		}
		var e Enterprise
		var m EnterpriseMember
		var charge EnterpriseCharge
		if err := lockForUpdate(tx).First(&e, snapshot.EnterpriseId).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("enterprise_id = ? AND user_id = ?", e.Id, snapshot.UserId).First(&m).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&charge, snapshot.Id).Error; err != nil {
			return err
		}
		if charge.Status == "refunded" {
			if refund {
				return nil
			}
			return ErrEnterpriseInput
		}
		charge.Status = "settled"
		if refund {
			charge.Status = "refunded"
			amount = 0
		}
		return applyEnterpriseCharge(tx, &e, &m, &charge, int64(amount), false)
	})
}

func SettleEnterpriseQuota(requestId string, amount int) error {
	return changeEnterpriseCharge(requestId, amount, false)
}
func RefundEnterpriseQuota(requestId string) error { return changeEnterpriseCharge(requestId, 0, true) }

func GetEnterpriseAvailableQuota(enterpriseId, userId int) (int, error) {
	var available int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		e, m, err := enterpriseAccess(tx, enterpriseId, userId, false)
		if err != nil {
			return err
		}
		available = e.Balance
		if m.QuotaLimit > 0 {
			available = min(available, m.QuotaLimit-m.UsedQuota)
		}
		return nil
	})
	return int(max(0, available)), err
}

func AdjustEnterpriseBalance(actorId, enterpriseId int, amount int64, reason string) error {
	if amount == 0 || amount > common.MaxWalletQuota || amount < -common.MaxWalletQuota || len(reason) == 0 || len(reason) > 256 {
		return ErrEnterpriseInput
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		e, m, err := enterpriseAccess(tx, enterpriseId, actorId, true)
		if err != nil {
			return err
		}
		if m.Role != "site_admin" {
			return ErrEnterpriseAccess
		}
		if (amount > 0 && e.Balance > common.MaxWalletQuota-amount) || (amount < 0 && e.Balance < -amount) {
			return ErrEnterpriseQuota
		}
		balance := e.Balance + amount
		if err := tx.Model(e).Update("balance", balance).Error; err != nil {
			return err
		}
		if err := tx.Create(&EnterpriseBalanceEntry{EnterpriseId: enterpriseId, ActorId: actorId, Amount: amount, Balance: balance, Reason: reason, CreatedAt: common.GetTimestamp()}).Error; err != nil {
			return err
		}
		return enterpriseAudit(tx, enterpriseId, actorId, 0, "balance.adjust", reason)
	})
}

// AdjustEnterpriseRequestQuota is used by incremental realtime and legacy billing.
// The caller owns delivery idempotency; ordinary requests use target settlement.
func AdjustEnterpriseRequestQuota(enterpriseId, userId, tokenId int, requestId string, delta int) error {
	if requestId == "" || delta > common.MaxWalletQuota || delta < -common.MaxWalletQuota {
		return ErrEnterpriseInput
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		e, m, err := enterpriseAccess(tx, enterpriseId, userId, false)
		if err != nil {
			return err
		}
		var charge EnterpriseCharge
		err = tx.Where("request_id = ?", requestId).First(&charge).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			charge = EnterpriseCharge{RequestId: requestId, EnterpriseId: enterpriseId, UserId: userId, TokenId: tokenId, Status: "settled", CreatedAt: common.GetTimestamp()}
		} else if err != nil {
			return err
		}
		if charge.EnterpriseId != enterpriseId || charge.UserId != userId || charge.TokenId != tokenId || charge.Status == "refunded" {
			return ErrEnterpriseInput
		}
		return applyEnterpriseCharge(tx, e, m, &charge, charge.Amount+int64(delta), true)
	})
}
