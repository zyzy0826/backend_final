package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"regs/internal/model"
	"regs/internal/repository"
)

type ProblemHandler struct {
	problemRepo *repository.ProblemRepository
}

func NewProblemHandler(problemRepo *repository.ProblemRepository) *ProblemHandler {
	return &ProblemHandler{problemRepo: problemRepo}
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

// Upsert handles PUT /api/problems — creates or updates a problem with its testcases.
func (h *ProblemHandler) Upsert(c *gin.Context) {
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

func (h *ProblemHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("problem_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid problem_id"})
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
	c.Status(http.StatusNoContent)
}

// GetTestcases returns all testcases for a problem (Admin only).
func (h *ProblemHandler) GetTestcases(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("problem_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid problem_id"})
		return
	}

	testcases, err := h.problemRepo.GetTestcases(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, testcases)
}
