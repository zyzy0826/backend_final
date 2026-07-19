package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"regs/internal/config"
	"regs/internal/judge"
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

	// Ensure the problem exists before accepting the upload.
	if _, err := h.problemRepo.FindByID(c.Request.Context(), problemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "problem not found"})
			return
		}
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
	if fileHeader.Size > int64(h.cfg.MaxUploadMB)<<20 {
		c.JSON(http.StatusRequestEntityTooLarge,
			gin.H{"error": fmt.Sprintf("file exceeds %d MB limit", h.cfg.MaxUploadMB)})
		return
	}

	operatorID := uuid.NewString()
	zipPath := filepath.Join(h.cfg.StoragePath, "uploads", operatorID+".zip")
	if err := c.SaveUploadedFile(fileHeader, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save uploaded file"})
		return
	}

	// Unpack the archive into the workspace immediately on upload. Both the ZIP
	// and the extracted files are kept; the judge reuses them and does not delete
	// the workspace afterwards. A failure here is non-fatal — the judge re-extracts
	// as a fallback and reports the error as a proper judge result.
	if err := judge.PrepareWorkspace(h.cfg, operatorID, zipPath); err != nil {
		log.Printf("submission %s: workspace pre-extract failed, will retry at judge time: %v", operatorID, err)
	}

	sub, err := h.submissionRepo.Create(c.Request.Context(), userID, problemID, operatorID, zipPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.queue.Push(queue.Job{
		SubmissionID: sub.ID,
		OperatorID:   operatorID,
		ProblemID:    problemID,
		ZipPath:      zipPath,
	})

	c.JSON(http.StatusAccepted, gin.H{"operator_id": operatorID})
}

// Rejudge handles POST /api/submissions/:operatorId/rejudge — resets the
// submission and pushes it back onto the job queue.
func (h *SubmissionHandler) Rejudge(c *gin.Context) {
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
	if detail.Status == model.StatusPending || detail.Status == model.StatusRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "submission is already queued or running"})
		return
	}
	if detail.SourcePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "source archive no longer available"})
		return
	}

	if err := h.submissionRepo.ResetForRejudge(c.Request.Context(), detail.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.queue.Push(queue.Job{
		SubmissionID: detail.ID,
		OperatorID:   operatorID,
		ProblemID:    detail.ProblemID,
		ZipPath:      detail.SourcePath,
	})

	c.JSON(http.StatusAccepted, gin.H{"operator_id": operatorID, "status": model.StatusPending})
}

var logFileNames = map[string]string{
	"configure": "configure.log",
	"compile":   "compile.log",
	"output":    "output.log",
}

// GetLog handles GET /api/submissions/:operatorId/logs/:phase — serves one log
// segment (configure|compile|output) from its physical file, falling back to
// the DB copy for legacy records.
func (h *SubmissionHandler) GetLog(c *gin.Context) {
	operatorID := c.Param("operatorId")
	phase := c.Param("phase")

	fileName, ok := logFileNames[phase]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phase must be one of: configure, compile, output"})
		return
	}

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

	logPath := filepath.Join(h.cfg.StoragePath, "logs", operatorID, fileName)
	if data, err := os.ReadFile(logPath); err == nil {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
		return
	}

	var content string
	switch phase {
	case "configure":
		content = detail.ConfigureLog
	case "compile":
		content = detail.CompileLog
	case "output":
		content = detail.OutputLog
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
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
