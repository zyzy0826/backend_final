package handler

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// Create handles POST /api/submissions — accepts a ZIP, enqueues a judge job,
// and returns operatorId immediately (202) without waiting for the result.
func (h *SubmissionHandler) Create(c *gin.Context) {
	userID := c.GetInt("user_id")

	problemID, err := strconv.Atoi(c.PostForm("problem_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or missing problem_id"})
		return
	}

	// Ensure the problem exists and pull its testcases + time limit up front.
	problem, err := h.problemRepo.FindByID(c.Request.Context(), problemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "problem not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	testcases, err := h.problemRepo.GetTestcases(c.Request.Context(), problemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}
	if !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".zip") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file must be a .zip archive"})
		return
	}

	operatorID := uuid.NewString()
	zipPath := filepath.Join(h.cfg.StoragePath, "uploads", operatorID+".zip")
	if err := c.SaveUploadedFile(fileHeader, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save uploaded file"})
		return
	}

	sub, err := h.submissionRepo.Create(c.Request.Context(), userID, problemID, operatorID, zipPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	timeLimit := problem.TimeLimit
	if timeLimit <= 0 {
		timeLimit = h.cfg.TimeLimitSeconds
	}

	h.queue.Push(queue.Job{
		SubmissionID: sub.ID,
		OperatorID:   operatorID,
		ProblemID:    problemID,
		ZipPath:      zipPath,
		Testcases:    testcases,
		TimeLimit:    timeLimit,
	})

	c.JSON(http.StatusAccepted, gin.H{"operator_id": operatorID})
}

// List handles GET /api/submissions — returns the authenticated user's submissions.
func (h *SubmissionHandler) List(c *gin.Context) {
	userID := c.GetInt("user_id")

	subs, err := h.submissionRepo.ListByUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, subs)
}

// Get handles GET /api/submissions/:operatorId — returns detail + three log segments.
func (h *SubmissionHandler) Get(c *gin.Context) {
	operatorID := c.Param("operatorId")

	detail, err := h.submissionRepo.FindByOperatorID(c.Request.Context(), operatorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "submission not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !h.canAccess(c, detail.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// GetSource handles GET /api/submissions/:operatorId/source — downloads the original ZIP.
func (h *SubmissionHandler) GetSource(c *gin.Context) {
	operatorID := c.Param("operatorId")

	detail, err := h.submissionRepo.FindByOperatorID(c.Request.Context(), operatorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "submission not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !h.canAccess(c, detail.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	if detail.SourcePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "source not available"})
		return
	}
	c.FileAttachment(detail.SourcePath, operatorID+".zip")
}

// GetByUser handles GET /api/users/:user_id/submissions (Guest-accessible).
func (h *SubmissionHandler) GetByUser(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	subs, err := h.submissionRepo.ListByUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, subs)
}

// canAccess returns true if the requester is the submission owner or an admin.
func (h *SubmissionHandler) canAccess(c *gin.Context, ownerID int) bool {
	if roleVal, ok := c.Get("role"); ok {
		if role, ok := roleVal.(model.Role); ok && role == model.RoleAdmin {
			return true
		}
	}
	return c.GetInt("user_id") == ownerID
}
