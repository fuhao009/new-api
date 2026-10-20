package middleware

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		id := common.NewRequestId()
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(c.Request.Context(), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		common.StartRelayTrace(c)
		c.Next()
		if trace := common.GetRelayTrace(c); trace != nil {
			trace.Mark("request_complete")
			if snapshot, err := common.Marshal(trace.Snapshot()); err == nil {
				logger.LogInfo(c, common.LogText("relay_trace=%s", snapshot))
			}
		}
	}
}
