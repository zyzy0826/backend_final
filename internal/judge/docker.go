package judge

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	TimedOut bool
}

type DockerRunner struct {
	cli   *client.Client
	image string
}

func NewDockerRunner(image string) (*DockerRunner, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &DockerRunner{cli: cli, image: image}, nil
}

// Run executes a command in a new container and returns its output.
// hostWorkspacePath must be the path on the HOST machine (not inside any container).
// networkMode: "bridge" for compile phase, "none" for execute phase.
func (d *DockerRunner) Run(ctx context.Context, hostWorkspacePath string, cmd []string, networkMode string, timeoutSec int) (*RunResult, error) {
	absPath, err := filepath.Abs(hostWorkspacePath)
	if err != nil {
		return nil, err
	}

	cfg := &container.Config{
		Image:      d.image,
		Cmd:        cmd,
		WorkingDir: "/workspace",
	}
	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: absPath,
				Target: "/workspace",
			},
		},
		NetworkMode: container.NetworkMode(networkMode),
	}

	resp, err := d.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}
	containerID := resp.ID
	// Always remove the container after use.
	defer d.cli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{Force: true})

	if err := d.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	statusCh, errCh := d.cli.ContainerWait(timeoutCtx, containerID, container.WaitConditionNotRunning)

	var exitCode int
	var timedOut bool

	select {
	case err := <-errCh:
		if timeoutCtx.Err() != nil {
			timedOut = true
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			d.cli.ContainerStop(stopCtx, containerID, container.StopOptions{})
		} else if err != nil {
			return nil, fmt.Errorf("wait container: %w", err)
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
	}

	logReader, err := d.cli.ContainerLogs(context.Background(), containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return &RunResult{ExitCode: exitCode, TimedOut: timedOut}, nil
	}
	defer logReader.Close()

	var stdout, stderr bytes.Buffer
	stdcopy.StdCopy(&stdout, &stderr, logReader)

	return &RunResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		TimedOut: timedOut,
	}, nil
}
