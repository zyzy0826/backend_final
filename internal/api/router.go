/* 設定 API 路由 */

package api

import (
	"crypto/ecdsa"

	"regs/internal/api/handler"
	"regs/internal/api/middleware"
	"regs/internal/model"

	"github.com/gin-gonic/gin"
)

func SetupRouter(
	publicKey *ecdsa.PublicKey,
	userHandler *handler.UserHandler,
	problemHandler *handler.ProblemHandler,
	submissionHandler *handler.SubmissionHandler,
	statsHandler *handler.StatsHandler,
) *gin.Engine {
	r := gin.Default()

	auth := middleware.Auth(publicKey)
	requireUser := middleware.RequireRole(model.RoleUser)
	requireAdmin := middleware.RequireRole(model.RoleAdmin)

	api := r.Group("/api")

	// UserInfo routes
	users := api.Group("/users")
	{
		users.POST("/register", userHandler.Register)
		users.POST("/login", userHandler.Login)
		users.POST("/logout", auth, userHandler.Logout)
		users.GET("/me", auth, userHandler.Me)
		users.GET("/:user_id/submissions", submissionHandler.GetByUser) // Guest-accessible
	}

	// Problem routes
	problems := api.Group("/problems")
	{
		problems.GET("", problemHandler.List)                                                   // Guest
		problems.GET("/:problem_id", problemHandler.Get)                                        // Guest
		problems.PUT("", auth, requireAdmin, problemHandler.Upsert)                             // Admin
		problems.DELETE("/:problem_id", auth, requireAdmin, problemHandler.Delete)              // Admin
		problems.GET("/:problem_id/testcases", auth, requireAdmin, problemHandler.GetTestcases) // Admin
	}

	// Submission routes
	submissions := api.Group("/submissions", auth, requireUser)
	{
		submissions.POST("", submissionHandler.Create)                      // User+
		submissions.GET("", submissionHandler.List)                         // User+
		submissions.GET("/:operatorId", submissionHandler.Get)              // User+ (owner or admin)
		submissions.GET("/:operatorId/source", submissionHandler.GetSource) // User+ (owner or admin)
	}

	// Stats routes (Guest-accessible)
	stats := api.Group("/stats")
	{
		stats.GET("/problems/:problem_id", statsHandler.ProblemStats)
		stats.GET("/users/:user_id", statsHandler.UserStats)
	}

	return r
}
