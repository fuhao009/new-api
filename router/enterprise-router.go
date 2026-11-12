package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func setEnterpriseRoutes(api *gin.RouterGroup) {
	group := api.Group("/enterprises", middleware.UserAuth(), middleware.DisableCache())
	group.GET("", controller.ListEnterprises)
	group.POST("", middleware.CriticalRateLimit(), controller.CreateEnterprise)
	group.GET("/:enterprise", controller.GetEnterprise)
	group.PATCH("/:enterprise", middleware.CriticalRateLimit(), controller.UpdateEnterprise)
	group.GET("/:enterprise/members", controller.ListEnterpriseMembers)
	group.POST("/:enterprise/members", middleware.CriticalRateLimit(), controller.AddEnterpriseMember)
	group.PATCH("/:enterprise/members/:target", middleware.CriticalRateLimit(), controller.UpdateEnterpriseMember)
	group.POST("/:enterprise/managed-accounts", middleware.CriticalRateLimit(), controller.CreateEnterpriseManagedAccount)
	group.POST("/:enterprise/owner-transfer", middleware.CriticalRateLimit(), controller.TransferEnterpriseOwner)
	group.POST("/:enterprise/balance", middleware.CriticalRateLimit(), controller.AdjustEnterpriseBalance)
	group.GET("/:enterprise/balance", controller.ListEnterpriseBalanceEntries)
	group.GET("/:enterprise/ledger", controller.ListEnterpriseLedger)
	group.GET("/:enterprise/audit", controller.ListEnterpriseAudit)
	group.GET("/:enterprise/keys", controller.ListEnterpriseKeys)
	group.POST("/:enterprise/keys", middleware.CriticalRateLimit(), controller.MutateEnterpriseKey)
	group.PATCH("/:enterprise/keys/:target", middleware.CriticalRateLimit(), controller.MutateEnterpriseKey)
	group.DELETE("/:enterprise/keys/:target", middleware.CriticalRateLimit(), controller.MutateEnterpriseKey)
}
