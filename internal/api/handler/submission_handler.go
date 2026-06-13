package handler

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"regs/internal/config"
	"regs/internal/model"
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
	userID := c.GetInt("user_id")

	problemID, err := strconv.Atoi(c.PostForm("problem_id"))
	if err != nil || problemID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid problem_id"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "zip file required"})
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .zip files accepted"})
		return
	}

	problem, err := h.problemRepo.FindByID(c.Request.Context(), problemID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "problem not found"})
		return
	}

	testcases, err := h.problemRepo.GetTestcases(c.Request.Context(), problemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	operatorID := uuid.New().String()
	uploadPath := filepath.Join(h.cfg.StoragePath, "uploads", operatorID+".zip")

	out, err := os.Create(uploadPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}
	out.Close()

	sub, err := h.submissionRepo.Create(c.Request.Context(), userID, problemID, operatorID, uploadPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create submission"})
		return
	}

	h.queue.Push(queue.Job{
		SubmissionID: sub.ID,
		OperatorID:   operatorID,
		ProblemID:    problemID,
		ZipPath:      uploadPath,
		Testcases:    testcases,
		TimeLimit:    problem.TimeLimit,
	})

	c.JSON(http.StatusAccepted, gin.H{"operator_id": operatorID})
}

// List handles GET /api/submissions — returns the authenticated user's submissions.
func (h *SubmissionHandler) List(c *gin.Context) {
	userID := c.GetInt("user_id")
	subs, err := h.submissionRepo.ListByUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, subs)
}

// Get handles GET /api/submissions/{operatorId} — returns detail + logs.
func (h *SubmissionHandler) Get(c *gin.Context) {
	operatorID := c.Param("operatorId")
	detail, err := h.submissionRepo.FindByOperatorID(c.Request.Context(), operatorID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "submission not found"})
		return
	}

	if !h.canAccess(c, detail.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// GetSource handles GET /api/submissions/{operatorId}/source — downloads the original ZIP.
func (h *SubmissionHandler) GetSource(c *gin.Context) {
	operatorID := c.Param("operatorId")
	detail, err := h.submissionRepo.FindByOperatorID(c.Request.Context(), operatorID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "submission not found"})
		return
	}

	if !h.canAccess(c, detail.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	c.FileAttachment(detail.SourcePath, operatorID+".zip")
}

// GetByUser handles GET /api/users/{user_id}/submissions (Guest-accessible).
func (h *SubmissionHandler) GetByUser(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}
	subs, err := h.submissionRepo.ListByUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, subs)
}

// canAccess returns true if the requester is the owner or an admin.
func (h *SubmissionHandler) canAccess(c *gin.Context, ownerID int) bool {
	role, _ := c.Get("role")
	if role == string(model.RoleAdmin) {
		return true
	}
	return c.GetInt("user_id") == ownerID
}
