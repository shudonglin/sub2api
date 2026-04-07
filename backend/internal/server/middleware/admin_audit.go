package middleware

import (
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AdminAuditLog logs all admin write operations for security audit trail.
// It captures: timestamp, admin user ID, auth method, HTTP method, path,
// status code, client IP, user agent, and request duration.
// Only POST, PUT, PATCH, DELETE requests are logged; read-only methods are skipped.
func AdminAuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only audit write operations
		method := c.Request.Method
		if method == "GET" || method == "HEAD" || method == "OPTIONS" {
			c.Next()
			return
		}

		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		// Extract admin identity from context (set by AdminAuthMiddleware)
		adminID := "unknown"
		if subject, ok := GetAuthSubjectFromContext(c); ok {
			adminID = fmt.Sprintf("%d", subject.UserID)
		}

		authMethod := "unknown"
		if method, exists := c.Get("auth_method"); exists {
			if s, ok := method.(string); ok {
				authMethod = s
			}
		}

		l := logger.FromContext(c.Request.Context())
		l.Info("ADMIN_AUDIT",
			zap.String("component", "admin.audit"),
			zap.String("admin_id", adminID),
			zap.String("auth_method", authMethod),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.String("ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	}
}
