package judge

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	TimedOut bool
}

type DockerRunner struct {
	image string
}

func NewDockerRunner(image string) (*DockerRunner, error) {
	return &DockerRunner{image: image}, nil
}

// Run starts a new Docker container, executes cmd, and returns its output.
// hostWorkspacePath must be the absolute path on the HOST machine.
// networkMode: "bridge" for compile phase, "none" for execute phase.
func (d *DockerRunner) Run(ctx context.Context, hostWorkspacePath string, cmd []string, networkMode string, timeoutSec int) (*RunResult, error) {
	absPath, err := filepath.Abs(hostWorkspacePath)
	if err != nil {
		return nil, err
	}

	mountSrc := absPath
	if runtime.GOOS == "windows" {
		mountSrc = toDockerPath(absPath)
	}

	args := []string{
		"run", "--rm",
		"--network", networkMode,
		"-v", mountSrc + ":/workspace",
		"-w", "/workspace",
		d.image,
	}
	args = append(args, cmd...)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	c := exec.CommandContext(timeoutCtx, "docker", args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	runErr := c.Run()
	timedOut := timeoutCtx.Err() != nil

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if !timedOut {
			return nil, fmt.Errorf("docker run: %w", runErr)
		}
	}

	return &RunResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		TimedOut: timedOut,
	}, nil
}

// toDockerPath converts a Windows path (e.g. D:\foo\bar) to Docker bind-mount format (/d/foo/bar).
func toDockerPath(winPath string) string {
	if len(winPath) >= 2 && winPath[1] == ':' {
		drive := strings.ToLower(string(winPath[0]))
		rest := strings.ReplaceAll(winPath[2:], "\\", "/")
		return "/" + drive + rest
	}
	return strings.ReplaceAll(winPath, "\\", "/")
}
