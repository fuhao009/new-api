package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"strings"
)

func BatchUpdateTokens(c *gin.Context) {
	var input struct {
		Ids            []int  `json:"ids"`
		Operation      string `json:"operation"`
		Category       string `json:"category"`
		RemainQuota    int    `json:"remain_quota"`
		UnlimitedQuota bool   `json:"unlimited_quota"`
		Group          string `json:"group"`
		ModelLimits    string `json:"model_limits"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	updates := map[string]any{}
	switch input.Operation {
	case "category":
		category := strings.TrimSpace(input.Category)
		if len(category) > 64 || category == "__uncategorized__" {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		updates["category"] = category
	case "quota":
		if input.RemainQuota < 0 || input.RemainQuota > maxTokenQuota() {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		updates["remain_quota"] = input.RemainQuota
		updates["unlimited_quota"] = input.UnlimitedQuota
	case "group":
		group, err := getTokenRequestUserGroup(c)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if input.Group != "" && input.Group != "auto" {
			if _, ok := service.GetUserUsableGroups(group)[input.Group]; !ok {
				common.ApiErrorT(c, "Unauthorized, insufficient privileges")
				return
			}
		}
		updates["group"] = input.Group
		updates["auto_groups"] = ""
		updates["cross_group_retry"] = false
	case "models":
		if len(input.ModelLimits) > 8192 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		updates["model_limits"] = strings.TrimSpace(input.ModelLimits)
		updates["model_limits_enabled"] = strings.TrimSpace(input.ModelLimits) != ""
	default:
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.BatchUpdatePersonalTokens(c.GetInt("id"), input.Ids, updates); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "token.update_batch", map[string]any{"ids": input.Ids, "operation": input.Operation})
	common.ApiSuccess(c, nil)
}

func FilterTokens(c *gin.Context) {
	page := common.GetPageQuery(c)
	tokens, total, err := model.FilterPersonalTokens(c.GetInt("id"), page.GetStartIdx(), page.GetPageSize(), c.Query("keyword"), c.Query("category"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": tokens, "total": total, "page": page.GetPage(), "page_size": page.GetPageSize()})
}
