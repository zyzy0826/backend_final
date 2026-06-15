package handler

import (
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
	// TODO: implement — parse :problem_id, return ProblemStats as JSON
}

func (h *StatsHandler) UserStats(c *gin.Context) {
	// TODO: implement — parse :user_id, return UserStats as JSON
}
