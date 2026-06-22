package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"regs/internal/repository"
)

type StatsHandler struct {
	submissionRepo *repository.SubmissionRepository
}

func NewStatsHandler(submissionRepo *repository.SubmissionRepository) *StatsHandler {
	return &StatsHandler{submissionRepo: submissionRepo}
}

func (h *StatsHandler) ProblemStats(c *gin.Context) {
	problemID, err := strconv.Atoi(c.Param("problem_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid problem_id"})
		return
	}

	stats, err := h.submissionRepo.GetProblemStats(c.Request.Context(), problemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *StatsHandler) UserStats(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	stats, err := h.submissionRepo.GetUserStats(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}
