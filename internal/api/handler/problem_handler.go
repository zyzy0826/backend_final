package handler

import (
	"archive/zip"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"regs/internal/config"
	"regs/internal/model"
	"regs/internal/repository"
)

type ProblemHandler struct {
	problemRepo *repository.ProblemRepository
	cfg         *config.Config
}

func NewProblemHandler(problemRepo *repository.ProblemRepository, cfg *config.Config) *ProblemHandler {
	return &ProblemHandler{problemRepo: problemRepo, cfg: cfg}
}

func (h *ProblemHandler) List(c *gin.Context) {
	problems, err := h.problemRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, problems)
}

func (h *ProblemHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("problem_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid problem_id"})
		return
	}

	p, err := h.problemRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "problem not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

// Upsert handles PUT /api/problems — creates or updates a problem.
//
//   - multipart/form-data: fields id/title/description/time_limit plus a `file`
//     problem package ZIP (CMakeLists.txt + spec cases) → test-based judging.
//   - application/json: metadata plus inline input/expected testcases → I/O judging.
func (h *ProblemHandler) Upsert(c *gin.Context) {
	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		h.upsertWithPackage(c)
		return
	}

	var req struct {
		ID          int              `json:"id"`
		Title       string           `json:"title" binding:"required"`
		Description string           `json:"description"`
		TimeLimit   int              `json:"time_limit"`
		Testcases   []model.Testcase `json:"testcases"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p := &model.Problem{
		ID:          req.ID,
		Title:       req.Title,
		Description: req.Description,
		TimeLimit:   req.TimeLimit,
	}

	result, err := h.problemRepo.Upsert(c.Request.Context(), p, req.Testcases)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ProblemHandler) upsertWithPackage(c *gin.Context) {
	title := c.PostForm("title")
	if title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing title"})
		return
	}
	id, _ := strconv.Atoi(c.PostForm("id"))                // optional: 0 → create new
	timeLimit, _ := strconv.Atoi(c.PostForm("time_limit")) // optional: 0 → server default

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file (problem package ZIP)"})
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

	// Save to a temp name first and validate before touching the DB.
	problemsDir := filepath.Join(h.cfg.StoragePath, "problems")
	tmpPath := filepath.Join(problemsDir, "tmp-"+uuid.NewString()+".zip")
	if err := c.SaveUploadedFile(fileHeader, tmpPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save uploaded file"})
		return
	}
	ok, err := zipHasCMakeLists(tmpPath)
	if err != nil || !ok {
		os.Remove(tmpPath)
		c.JSON(http.StatusBadRequest, gin.H{"error": "package ZIP must contain CMakeLists.txt at its root"})
		return
	}

	p := &model.Problem{
		ID:          id,
		Title:       title,
		Description: c.PostForm("description"),
		TimeLimit:   timeLimit,
	}
	result, err := h.problemRepo.Upsert(c.Request.Context(), p, nil)
	if err != nil {
		os.Remove(tmpPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	finalPath := filepath.Join(problemsDir, fmt.Sprintf("%d.zip", result.ID))
	os.Remove(finalPath) // replace any previous package
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store package: " + err.Error()})
		return
	}
	if err := h.problemRepo.UpdatePackagePath(c.Request.Context(), result.ID, finalPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result.PackagePath = finalPath
	result.ResolveJudgeMode()
	c.JSON(http.StatusOK, result)
}

func (h *ProblemHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("problem_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid problem_id"})
		return
	}

	// Look up first so the package file can be removed alongside the row.
	p, err := h.problemRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "problem not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.problemRepo.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "problem not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if p.PackagePath != "" {
		os.Remove(p.PackagePath)
	}
	c.Status(http.StatusNoContent)
}

// GetTestcases handles GET /api/problems/:problem_id/testcases (Admin only).
// Test-based problems download the package ZIP; I/O problems return JSON testcases.
func (h *ProblemHandler) GetTestcases(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("problem_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid problem_id"})
		return
	}

	p, err := h.problemRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "problem not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if p.PackagePath != "" {
		c.FileAttachment(p.PackagePath, fmt.Sprintf("problem-%d.zip", id))
		return
	}

	testcases, err := h.problemRepo.GetTestcases(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, testcases)
}

// zipHasCMakeLists reports whether the archive carries a CMakeLists.txt at its
// root or inside a single top-level wrapper folder.
func zipHasCMakeLists(zipPath string) (bool, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return false, err
	}
	defer r.Close()

	for _, f := range r.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if name == "CMakeLists.txt" ||
			(strings.HasSuffix(name, "/CMakeLists.txt") && strings.Count(name, "/") == 1) {
			return true, nil
		}
	}
	return false, nil
}
