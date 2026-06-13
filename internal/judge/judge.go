package judge

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
func (j *Judge) RunJob(ctx context.Context, job JobInput) {
	_ = j.subRepo.UpdateStatus(ctx, job.SubmissionID, model.StatusRunning)

	workspacePath := filepath.Join(j.cfg.StoragePath, "workspace", job.OperatorID)
	hostWorkspacePath := filepath.Join(j.cfg.HostStoragePath, "workspace", job.OperatorID)

	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		j.done(ctx, job.SubmissionID, model.StatusSE, "", "", "failed to create workspace")
		return
	}
	defer os.RemoveAll(workspacePath)

	if err := extractZip(job.ZipPath, workspacePath); err != nil {
		j.done(ctx, job.SubmissionID, model.StatusSE, "", "", "unzip failed: "+err.Error())
		return
	}

	if _, err := os.Stat(filepath.Join(workspacePath, "CMakeLists.txt")); os.IsNotExist(err) {
		j.done(ctx, job.SubmissionID, model.StatusSE, "", "", "CMakeLists.txt not found")
		return
	}

	// Phase 1: cmake configure
	configResult, err := j.runner.Run(ctx, hostWorkspacePath,
		[]string{"cmake", "-G", "Ninja", "-B", "build"},
		"bridge", 60,
	)
	configLog := logFrom(configResult)
	if err != nil || configResult == nil || configResult.ExitCode != 0 {
		j.done(ctx, job.SubmissionID, model.StatusSE, configLog, "", "")
		return
	}

	// Phase 2: cmake build
	buildResult, err := j.runner.Run(ctx, hostWorkspacePath,
		[]string{"cmake", "--build", "build", "--verbose"},
		"bridge", 120,
	)
	compileLog := logFrom(buildResult)
	if err != nil || buildResult == nil || buildResult.ExitCode != 0 {
		j.done(ctx, job.SubmissionID, model.StatusCE, configLog, compileLog, "")
		return
	}

	// Phase 3: execute and judge each testcase
	timeLimit := job.TimeLimit
	if timeLimit <= 0 {
		timeLimit = j.cfg.TimeLimitSeconds
	}

	var outputLog strings.Builder
	finalStatus := model.StatusAC

	for i, tc := range job.Testcases {
		inputPath := filepath.Join(workspacePath, "input.txt")
		if err := os.WriteFile(inputPath, []byte(tc.Input), 0644); err != nil {
			finalStatus = model.StatusRE
			break
		}

		// Find the binary and run it with stdin redirected from input.txt (network=none)
		execCmd := []string{"sh", "-c",
			"BINARY=$(find /workspace/build -maxdepth 3 -type f -executable " +
				"! -path '*/CMakeFiles/*' | head -1) && " +
				"[ -n \"$BINARY\" ] && \"$BINARY\" < /workspace/input.txt",
		}
		execResult, err := j.runner.Run(ctx, hostWorkspacePath, execCmd, "none", timeLimit)
		if err != nil {
			finalStatus = model.StatusRE
			break
		}

		fmt.Fprintf(&outputLog, "=== Testcase %d ===\n%s\n", i+1, execResult.Stdout)

		if execResult.TimedOut {
			finalStatus = model.StatusTLE
			break
		}
		if execResult.ExitCode != 0 {
			finalStatus = model.StatusRE
			break
		}

		got := strings.TrimSpace(execResult.Stdout)
		want := strings.TrimSpace(tc.Expected)
		if got != want {
			finalStatus = model.StatusWA
			break
		}
	}

	j.done(ctx, job.SubmissionID, finalStatus, configLog, compileLog, outputLog.String())
}

func (j *Judge) done(ctx context.Context, id int, status model.Status, configLog, compileLog, outputLog string) {
	_ = j.subRepo.UpdateStatusWithLogs(ctx, id, status, configLog, compileLog, outputLog)
}

func logFrom(r *RunResult) string {
	if r == nil {
		return ""
	}
	return r.Stdout + r.Stderr
}

func extractZip(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	destPath = filepath.Clean(destPath) + string(os.PathSeparator)

	for _, f := range r.File {
		fpath := filepath.Join(destPath, f.Name)

		// Prevent zip-slip path traversal
		if !strings.HasPrefix(filepath.Clean(fpath)+string(os.PathSeparator), destPath) {
			return fmt.Errorf("illegal path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		out, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}

		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}
