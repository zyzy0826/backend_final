package judge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	TimedOut bool
}

// Mount describes how a submission's workspace is handed to a judge container:
// the `docker run` flags that expose it, and the path it lands on inside the
// container.
type Mount struct {
	Args []string // e.g. ["-v", "regs-storage:/storage"]
	Dir  string   // e.g. "/workspace" or "/storage/workspace/<operatorId>"
}

type DockerRunner struct {
	image  string
	memory string // applied to limited (execute-phase) containers, e.g. "512m"
	cpus   string // e.g. "1.0"

	// volumeSubpath records whether the daemon can mount a single directory out
	// of a named volume; see supportsVolumeSubpath.
	volumeSubpath bool
}

func NewDockerRunner(image, memory, cpus string) (*DockerRunner, error) {
	return &DockerRunner{
		image:         image,
		memory:        memory,
		cpus:          cpus,
		volumeSubpath: supportsVolumeSubpath(),
	}, nil
}

// SupportsVolumeSubpath reports whether judge containers can be given just their
// own directory out of the shared storage volume.
func (d *DockerRunner) SupportsVolumeSubpath() bool { return d.volumeSubpath }

// Run starts a new Docker container, executes cmd, and returns its output.
// networkMode: "bridge" for the configure/build phases, "none" for the execute phase.
// limited applies memory/CPU/pids caps — enable it for untrusted student binaries.
func (d *DockerRunner) Run(ctx context.Context, mnt Mount, cmd []string, networkMode string, timeoutSec int, limited bool) (*RunResult, error) {
	// A known container name lets us kill the container on timeout: killing the
	// docker CLI client alone would leave the container running in the background.
	name := "regs-judge-" + randomSuffix()

	args := []string{
		"run", "--rm",
		"--name", name,
		"--network", networkMode,
	}
	args = append(args, mnt.Args...)
	args = append(args, "-w", mnt.Dir)
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

// EnsureImage makes sure the judge image is available to the Docker daemon,
// pulling it if it is not. Doing this once at startup means the first
// submission does not silently fail (or stall) on a missing image, and keeps
// the deployment self-contained: nothing has to be pulled by hand beforehand.
func (d *DockerRunner) EnsureImage(ctx context.Context) (pulled bool, err error) {
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", d.image).Run(); err == nil {
		return false, nil
	}
	out, err := exec.CommandContext(ctx, "docker", "pull", d.image).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("docker pull %s: %w: %s", d.image, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// volumeSubpathMinMajor is the first Docker major version that supports
// `--mount ...,volume-subpath=`.
const volumeSubpathMinMajor = 25

// supportsVolumeSubpath reports whether `--mount ...,volume-subpath=` can be
// used. Both ends have to be new enough: the CLI parses the flag (an older
// client rejects it outright, whatever the daemon supports) and the daemon
// implements it.
//
// It matters because the app and the judge containers share one named storage
// volume. Without subpath support a judge container has to mount the whole
// volume, which would expose every other submission's sources to the student
// binary being executed; with it, each container sees only its own workspace.
func supportsVolumeSubpath() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "version",
		"--format", "{{.Client.Version}} {{.Server.Version}}").Output()
	if err != nil {
		return false
	}

	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return false
	}
	for _, v := range fields {
		if majorVersion(v) < volumeSubpathMinMajor {
			return false
		}
	}
	return true
}

// majorVersion parses the leading major number of a version string such as
// "28.3.3", returning 0 if it cannot be read.
func majorVersion(v string) int {
	major, _, ok := strings.Cut(strings.TrimSpace(v), ".")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
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
