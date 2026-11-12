package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"slices"
)

// BatchUpdatePersonalTokens rejects mixed ownership before changing any token.
func BatchUpdatePersonalTokens(userId int, ids []int, updates map[string]any) error {
	if len(ids) == 0 || len(ids) > 100 {
		return errors.New("Select between 1 and 100 API keys")
	}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	for field := range updates {
		if !slices.Contains([]string{"category", "remain_quota", "unlimited_quota", "group", "auto_groups", "cross_group_retry", "model_limits", "model_limits_enabled"}, field) {
			return errors.New("Invalid parameters")
		}
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var tokens []Token
		if err := lockForUpdate(tx).Where("user_id = ? AND enterprise_id = 0 AND id IN ?", userId, ids).Find(&tokens).Error; err != nil {
			return err
		}
		if len(tokens) != len(ids) {
			return common.NewMessage("Unauthorized, insufficient privileges")
		}
		if err := invalidateTokensCache(tokens); err != nil {
			return err
		}
		return tx.Model(&Token{}).Where("user_id = ? AND enterprise_id = 0 AND id IN ?", userId, ids).Updates(updates).Error
	})
}

func FilterPersonalTokens(userId, offset, limit int, keyword, category string) ([]Token, int64, error) {
	var tokens []Token
	var total int64
	query := DB.Model(&Token{}).Where("user_id = ? AND enterprise_id = 0", userId)
	if category == "__uncategorized__" {
		query = query.Where("category = ?", "")
	} else if category != "" {
		query = query.Where("category = ?", category)
	}
	if keyword != "" {
		pattern, err := sanitizeLikePattern(keyword)
		if err != nil {
			return nil, 0, err
		}
		query = query.Where("name LIKE ? ESCAPE '!'", pattern)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(max(0, offset)).Limit(min(100, max(1, limit))).Find(&tokens).Error
	for i := range tokens {
		tokens[i].Key = MaskTokenKey(tokens[i].Key)
	}
	return tokens, total, err
}
