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
// Role hierarchy: Admin (2) > User (1). Guest routes should have no middleware.
// This must run after Auth, which places the role into the gin context.
func RequireRole(minRole model.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		roleVal, exists := c.Get("role")
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		role, ok := roleVal.(model.Role)
		if !ok || roleLevel[role] < roleLevel[minRole] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}
