package middleware

import (
	"github.com/gin-gonic/gin"
	"regs/internal/model"
)

var roleLevel = map[model.Role]int{
	model.RoleUser:  1,
	model.RoleAdmin: 2,
}

// RequireRole aborts with 403 if the authenticated user's role is below minRole.
// Role hierarchy: Admin (2) > User (1). Guest routes should have no middleware.
func RequireRole(minRole model.Role) gin.HandlerFunc {
	// TODO: implement — read "role" from context, compare against minRole level, abort with 403 if insufficient
	return func(c *gin.Context) {
		c.Next()
	}
}
