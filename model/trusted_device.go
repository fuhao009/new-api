package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type TrustedDevice struct {
	Id          int    `json:"id"`
	UserId      int    `json:"-" gorm:"index"`
	SecretHash  string `json:"-" gorm:"size:64;uniqueIndex"`
	AuthVersion int64  `json:"-"`
	Name        string `json:"name" gorm:"size:128"`
	UserAgent   string `json:"user_agent" gorm:"size:512"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at" gorm:"index"`
	LastUsedAt  int64  `json:"last_used_at"`
	WindowStart int64  `json:"-"`
	Attempts    int    `json:"-"`
}

type PasswordLoginGuard struct {
	UserId      int `gorm:"primaryKey;autoIncrement:false"`
	WindowStart int64
	Failures    int
	LockedUntil int64
}

func trustedDeviceHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func EnrollTrustedDevice(identity AuthSessionIdentity, name, agent string) (string, error) {
	userId := identity.UserID
	if len(name) == 0 || len(name) > 128 {
		return "", errors.New("Invalid parameters")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	raw := hex.EncodeToString(secret)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var user User
		if err := lockForUpdate(tx).First(&user, userId).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return ErrInvalidCredentials
		}
		var count int64
		if err := tx.Model(&TrustedDevice{}).Where("user_id = ? AND expires_at > ?", userId, time.Now().Unix()).Count(&count).Error; err != nil {
			return err
		}
		if count >= 20 {
			return errors.New("Trusted device limit reached")
		}
		if len(agent) > 512 {
			agent = agent[:512]
		}
		return tx.Create(&TrustedDevice{UserId: userId, SecretHash: trustedDeviceHash(raw), AuthVersion: user.AuthVersion, Name: name, UserAgent: agent, CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix()}).Error
	})
	return raw, err
}

// CheckPasswordLoginGuard never grants authentication. It only applies lockout
// policy after password verification; MFA remains in the normal login pipeline.
func CheckPasswordLoginGuard(user *User, passwordValid bool, rawDevice string) error {
	if user.Id <= 0 {
		return ErrInvalidCredentials
	}
	allowed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current User
		if err := lockForUpdate(tx).First(&current, user.Id).Error; err != nil {
			return err
		}
		now := time.Now().Unix()
		trusted := false
		if len(rawDevice) == 64 {
			var device TrustedDevice
			err := lockForUpdate(tx).Where("user_id = ? AND secret_hash = ? AND auth_version = ? AND expires_at > ?", user.Id, trustedDeviceHash(rawDevice), current.AuthVersion, now).First(&device).Error
			if err == nil {
				if now-device.WindowStart >= 60 {
					device.WindowStart = now
					device.Attempts = 0
				}
				device.Attempts++
				trusted = device.Attempts <= 5
				if passwordValid && trusted {
					device.LastUsedAt = now
				}
				if err := tx.Save(&device).Error; err != nil {
					return err
				}
				if !trusted {
					return nil
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		guard := PasswordLoginGuard{UserId: user.Id}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&guard).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&guard, "user_id = ?", user.Id).Error; err != nil {
			return err
		}
		if now-guard.WindowStart >= 900 && guard.LockedUntil <= now {
			guard.WindowStart = now
			guard.Failures = 0
			guard.LockedUntil = 0
		}
		if passwordValid && (guard.LockedUntil <= now || trusted) && current.Status == common.UserStatusEnabled && current.AuthVersion == user.AuthVersion {
			allowed = true
			guard.Failures = 0
			guard.LockedUntil = 0
		}
		if !passwordValid {
			guard.Failures++
			if guard.Failures >= 10 {
				guard.LockedUntil = max(guard.LockedUntil, now+900)
			}
		}
		return tx.Save(&guard).Error
	})
	if err != nil {
		return err
	}
	if !allowed {
		return ErrInvalidCredentials
	}
	return nil
}
