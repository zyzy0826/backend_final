package judge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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
	image  string
	memory string // applied to limited (execute-phase) containers, e.g. "512m"
	cpus   string // e.g. "1.0"
}

func NewDockerRunner(image, memory, cpus string) (*DockerRunner, error) {
	return &DockerRunner{image: image, memory: memory, cpus: cpus}, nil
}

// Run starts a new Docker container, executes cmd, and returns its output.
// hostWorkspacePath must be the absolute path on the HOST machine; it is mounted
// at /workspace inside the container.
// networkMode: "bridge" for the configure/build phases, "none" for the execute phase.
// limited applies memory/CPU/pids caps — enable it for untrusted student binaries.
func (d *DockerRunner) Run(ctx context.Context, hostWorkspacePath string, cmd []string, networkMode string, timeoutSec int, limited bool) (*RunResult, error) {
	absPath, err := filepath.Abs(hostWorkspacePath)
	if err != nil {
		return nil, err
	}

	mountSrc := absPath
	if runtime.GOOS == "windows" {
		mountSrc = toDockerPath(absPath)
	}

	// A known container name lets us kill the container on timeout: killing the
	// docker CLI client alone would leave the container running in the background.
	name := "regs-judge-" + randomSuffix()

	args := []string{
		"run", "--rm",
		"--name", name,
		"--network", networkMode,
		"-v", mountSrc + ":/workspace",
		"-w", "/workspace",
	}
	if limited {
		if d.memory != "" {
			args = append(args, "--memory", d.memory)
		}
		if d.cpus != "" {
			args = append(args, "--cpus", d.cpus)
		}
		args = append(args, "--pids-limit", "256")
	}
	args = append(args, d.image)
	args = append(args, cmd...)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	c := exec.CommandContext(timeoutCtx, "docker", args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	runErr := c.Run()
	timedOut := timeoutCtx.Err() != nil

	if timedOut {
		// The CLI process was killed, but the container itself keeps running;
		// stop it explicitly (--rm then removes it). Best-effort: it may have
		// already exited.
		killCtx, killCancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = exec.CommandContext(killCtx, "docker", "kill", name).Run()
		killCancel()
	}

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

func randomSuffix() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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
