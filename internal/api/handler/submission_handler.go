package handler

import (
	"github.com/gin-gonic/gin"
	"regs/internal/config"
	"regs/internal/queue"
	"regs/internal/repository"
)

type SubmissionHandler struct {
	submissionRepo *repository.SubmissionRepository
	problemRepo    *repository.ProblemRepository
	queue          *queue.Queue
	cfg            *config.Config
}

func NewSubmissionHandler(
	submissionRepo *repository.SubmissionRepository,
	problemRepo *repository.ProblemRepository,
	q *queue.Queue,
	cfg *config.Config,
) *SubmissionHandler {
	return &SubmissionHandler{
		submissionRepo: submissionRepo,
		problemRepo:    problemRepo,
		queue:          q,
		cfg:            cfg,
	}
}

// Create handles POST /api/submissions — accepts a ZIP, enqueues a judge job, returns operatorId immediately.
func (h *SubmissionHandler) Create(c *gin.Context) {
	// TODO: implement
	// 1. Get user_id from context
	// 2. Parse problem_id from form
	// 3. Accept multipart file (must be .zip)
	// 4. Look up problem and testcases
	// 5. Generate a UUID as operatorID
	// 6. Save ZIP to cfg.StoragePath/uploads/{operatorID}.zip
	// 7. submissionRepo.Create to get submission ID
	// 8. queue.Push the job
	// 9. Return 202 {"operator_id": "..."}
}

// List handles GET /api/submissions — returns the authenticated user's submissions.
func (h *SubmissionHandler) List(c *gin.Context) {
	// TODO: implement
}

// Get handles GET /api/submissions/:operatorId — returns detail + logs.
func (h *SubmissionHandler) Get(c *gin.Context) {
	// TODO: implement — check canAccess before returning
}

// GetSource handles GET /api/submissions/:operatorId/source — downloads the original ZIP.
func (h *SubmissionHandler) GetSource(c *gin.Context) {
	// TODO: implement — check canAccess, then c.FileAttachment
}

// GetByUser handles GET /api/users/:user_id/submissions (Guest-accessible).
func (h *SubmissionHandler) GetByUser(c *gin.Context) {
	// TODO: implement
}

// canAccess returns true if the requester is the submission owner or an admin.
func (h *SubmissionHandler) canAccess(c *gin.Context, ownerID int) bool {
	// TODO: implement
	return false
}
