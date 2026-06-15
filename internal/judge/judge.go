package judge

import (
	"context"

	"regs/internal/config"
	"regs/internal/model"
	"regs/internal/repository"
)

type Judge struct {
	runner  *DockerRunner
	subRepo *repository.SubmissionRepository
	cfg     *config.Config
}

func New(runner *DockerRunner, subRepo *repository.SubmissionRepository, cfg *config.Config) *Judge {
	return &Judge{runner: runner, subRepo: subRepo, cfg: cfg}
}

type JobInput struct {
	SubmissionID int
	OperatorID   string
	ProblemID    int
	ZipPath      string
	Testcases    []model.Testcase
	TimeLimit    int
}

// RunJob executes the full judge pipeline for a single submission.
// Pipeline: set status=running → extract ZIP → check CMakeLists.txt →
//
//	Phase1: cmake -G Ninja -B build (network=bridge, SE on fail) →
//	Phase2: cmake --build build (network=bridge, CE on fail) →
//	Phase3: run binary per testcase (network=none, AC/WA/RE/TLE)
func (j *Judge) RunJob(ctx context.Context, job JobInput) {
	// TODO: implement
}

func (j *Judge) done(ctx context.Context, id int, status model.Status, configureLog, compileLog, outputLog string) {
	// TODO: implement (call UpdateStatusWithLogs)
}

func logFrom(r *RunResult) string {
	// TODO: implement (combine Stdout + Stderr from RunResult)
	return ""
}

// extractZip extracts a ZIP archive to destPath.
// Must prevent zip-slip path traversal attacks by validating each file path prefix.
func extractZip(zipPath, destPath string) error {
	// TODO: implement with zip-slip prevention
	return nil
}
