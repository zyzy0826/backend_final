package handler

import (
	"github.com/gin-gonic/gin"
	"regs/internal/repository"
)

type ProblemHandler struct {
	problemRepo *repository.ProblemRepository
}

func NewProblemHandler(problemRepo *repository.ProblemRepository) *ProblemHandler {
	return &ProblemHandler{problemRepo: problemRepo}
}

func (h *ProblemHandler) List(c *gin.Context) {
	// TODO: implement — return all problems as JSON
}

func (h *ProblemHandler) Get(c *gin.Context) {
	// TODO: implement — parse :problem_id, return problem or 404
}

// Upsert handles PUT /api/problems — creates or updates a problem with its testcases.
func (h *ProblemHandler) Upsert(c *gin.Context) {
	// TODO: implement
	// 1. Bind JSON (id, title, description, time_limit, testcases)
	// 2. Call problemRepo.Upsert
	// 3. Return updated problem
}

func (h *ProblemHandler) Delete(c *gin.Context) {
	// TODO: implement — parse :problem_id, call problemRepo.Delete, return 204
}

// GetTestcases returns all testcases for a problem (Admin only).
func (h *ProblemHandler) GetTestcases(c *gin.Context) {
	// TODO: implement — parse :problem_id, return testcases as JSON
}
