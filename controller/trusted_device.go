package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

const trustedDeviceCookie = "newapi_trusted_device"

func ListTrustedDevices(c *gin.Context) {
	if _, ok := requireBrowserSession(c); !ok {
		return
	}
	var devices []model.TrustedDevice
	if err := model.DB.Where("user_id = ? AND expires_at > ?", c.GetInt("id"), time.Now().Unix()).Order("id desc").Find(&devices).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, devices)
}

func EnrollTrustedDevice(c *gin.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if common.DecodeJson(c.Request.Body, &input) != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTrustedDevice}) == nil {
		return
	}
	raw, err := model.EnrollTrustedDevice(identity, input.Name, c.Request.UserAgent())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: trustedDeviceCookie, Value: raw, Path: "/api/user/login", MaxAge: 30 * 24 * 3600, Expires: time.Now().Add(30 * 24 * time.Hour), HttpOnly: true, Secure: common.SessionCookieSecure, SameSite: http.SameSiteStrictMode})
	recordUserSecurityAudit(c, c.GetInt("id"), "trusted_device.enroll", nil)
	common.ApiSuccess(c, nil)
}

func RevokeTrustedDevice(c *gin.Context) {
	if _, ok := requireBrowserSession(c); !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	result := model.DB.Where("id = ? AND user_id = ?", id, c.GetInt("id")).Delete(&model.TrustedDevice{})
	if result.Error != nil {
		common.ApiError(c, result.Error)
		return
	}
	recordUserSecurityAudit(c, c.GetInt("id"), "trusted_device.revoke", map[string]any{"id": id})
	common.ApiSuccess(c, nil)
}
