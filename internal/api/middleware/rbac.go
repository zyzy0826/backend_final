package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"regs/internal/model"
)

var roleLevel = map[model.Role]int{
	model.RoleUser:  1,
	model.RoleAdmin: 2,
}

// RequireRole aborts with 403 if the authenticated user's role is below minRole.
// Role hierarchy: Admin > User. Guest routes should have no middleware.
func RequireRole(minRole model.Role) gin.HandlerFunc {
	minLevel := roleLevel[minRole]
	return func(c *gin.Context) {
		roleStr, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			c.Abort()
			return
		}
		if roleLevel[model.Role(roleStr.(string))] < minLevel {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		c.Next()
	}
}
