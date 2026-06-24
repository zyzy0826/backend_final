package judge

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log"
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

const (
	configureTimeout = 60  // seconds for `cmake -G`
	compileTimeout   = 120 // seconds for `cmake --build`
)

// findExecutable is a small shell snippet run inside the container that locates the
// first freshly-built executable under build/ and runs it with stdin from input.txt.
const runScript = `exe=$(find build -maxdepth 4 -type f -perm -u+x ` +
	`-not -path '*/CMakeFiles/*' -not -name '*.sh' -not -name '*.cmake' -not -name '*.so' | head -n1); ` +
	`if [ -z "$exe" ]; then echo "no executable found in build/" >&2; exit 127; fi; ` +
	`"$exe" < input.txt`

// RunJob executes the full judge pipeline for a single submission.
// Pipeline: set status=running → extract ZIP → check CMakeLists.txt →
//
//	Phase1: cmake -G Ninja -B build  (network=bridge, SE on fail) →
//	Phase2: cmake --build build      (network=bridge, CE on fail) →
//	Phase3: run binary per testcase  (network=none, AC/WA/RE/TLE)
func (j *Judge) RunJob(ctx context.Context, job JobInput) {
	if err := j.subRepo.UpdateStatus(ctx, job.SubmissionID, model.StatusRunning); err != nil {
		log.Printf("judge: mark running for submission %d: %v", job.SubmissionID, err)
	}

	// Per-submission workspace; always cleaned up when the job finishes.
	workspace := filepath.Join(j.cfg.StoragePath, "workspace", job.OperatorID)
	if err := os.MkdirAll(workspace, 0755); err != nil {
		j.done(ctx, job.SubmissionID, model.StatusSE, "failed to create workspace: "+err.Error(), "", "")
		return
	}
	defer os.RemoveAll(workspace)

	if err := extractZip(job.ZipPath, workspace); err != nil {
		j.done(ctx, job.SubmissionID, model.StatusSE, "failed to extract ZIP: "+err.Error(), "", "")
		return
	}

	// Locate the directory holding CMakeLists.txt (either the workspace root or a
	// single top-level folder inside the archive).
	projectRoot, err := findCMakeRoot(workspace)
	if err != nil {
		j.done(ctx, job.SubmissionID, model.StatusSE, "CMakeLists.txt not found in project root", "", "")
		return
	}
	// The Docker daemon mounts a path on the HOST machine, which differs from the
	// app's local path when running inside a container (DooD).
	hostProjectRoot := j.hostPath(projectRoot)

	// --- Phase 1: configure (cmake -G Ninja -B build) ---
	r1, err := j.runner.Run(ctx, hostProjectRoot,
		[]string{"cmake", "-G", "Ninja", "-B", "build"}, "bridge", configureTimeout)
	configureLog := logFrom(r1)
	if err != nil {
		configureLog += "\n[runner error] " + err.Error()
	}
	if err != nil || r1 == nil || r1.ExitCode != 0 {
		j.done(ctx, job.SubmissionID, model.StatusSE, configureLog, "", "")
		return
	}

	// --- Phase 2: build (cmake --build build --verbose) ---
	r2, err := j.runner.Run(ctx, hostProjectRoot,
		[]string{"cmake", "--build", "build", "--verbose"}, "bridge", compileTimeout)
	compileLog := logFrom(r2)
	if err != nil {
		compileLog += "\n[runner error] " + err.Error()
	}
	if err != nil || r2 == nil || r2.ExitCode != 0 {
		j.done(ctx, job.SubmissionID, model.StatusCE, configureLog, compileLog, "")
		return
	}

	// --- Phase 3: run each testcase in a fully network-isolated container ---
	finalStatus := model.StatusAC
	var outputLog strings.Builder

	if len(job.Testcases) == 0 {
		outputLog.WriteString("[warning] problem has no testcases; treated as AC after successful build\n")
	}

	for i, tc := range job.Testcases {
		outputLog.WriteString(fmt.Sprintf("=== Testcase %d ===\n", i+1))

		// Provide the testcase input to the program via a mounted file.
		inputPath := filepath.Join(projectRoot, "input.txt")
		if err := os.WriteFile(inputPath, []byte(tc.Input), 0644); err != nil {
			finalStatus = model.StatusRE
			outputLog.WriteString("failed to write input: " + err.Error() + "\n")
			break
		}

		r, err := j.runner.Run(ctx, hostProjectRoot,
			[]string{"sh", "-c", runScript}, "none", job.TimeLimit)
		outputLog.WriteString(logFrom(r))

		switch {
		case r != nil && r.TimedOut:
			finalStatus = model.StatusTLE
			outputLog.WriteString("\n[TLE] execution exceeded time limit\n")
		case err != nil:
			finalStatus = model.StatusRE
			outputLog.WriteString("\n[runner error] " + err.Error() + "\n")
		case r.ExitCode != 0:
			finalStatus = model.StatusRE
			outputLog.WriteString(fmt.Sprintf("\n[RE] non-zero exit code: %d\n", r.ExitCode))
		case !matchOutput(r.Stdout, tc.Expected):
			finalStatus = model.StatusWA
			outputLog.WriteString(fmt.Sprintf("\n[WA]\n--- expected ---\n%s\n--- got ---\n%s\n", tc.Expected, r.Stdout))
		default:
			outputLog.WriteString("\n[AC]\n")
			continue
		}
		// Any non-AC result short-circuits the remaining testcases.
		break
	}

	j.done(ctx, job.SubmissionID, finalStatus, configureLog, compileLog, outputLog.String())
}

func (j *Judge) done(ctx context.Context, id int, status model.Status, configureLog, compileLog, outputLog string) {
	if err := j.subRepo.UpdateStatusWithLogs(ctx, id, status, configureLog, compileLog, outputLog); err != nil {
		log.Printf("judge: failed to persist result for submission %d: %v", id, err)
	}
}

// hostPath maps a path under the app's local StoragePath onto the equivalent path on
// the Docker host (HostStoragePath). When running locally the two are identical.
func (j *Judge) hostPath(localPath string) string {
	rel, err := filepath.Rel(j.cfg.StoragePath, localPath)
	if err != nil {
		return localPath
	}
	return filepath.Join(j.cfg.HostStoragePath, rel)
}

func logFrom(r *RunResult) string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(r.Stdout)
	if r.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(r.Stdout, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(r.Stderr)
	}
	return b.String()
}

// matchOutput compares program output against the expected answer, ignoring
// trailing whitespace on each line and trailing blank lines (a common OJ convention).
func matchOutput(got, expected string) bool {
	return normalize(got) == normalize(expected)
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// findCMakeRoot returns the directory containing CMakeLists.txt: either the workspace
// itself, or a single top-level subdirectory (archives that wrap everything in a folder).
func findCMakeRoot(workspace string) (string, error) {
	if fileExists(filepath.Join(workspace, "CMakeLists.txt")) {
		return workspace, nil
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			sub := filepath.Join(workspace, e.Name())
			if fileExists(filepath.Join(sub, "CMakeLists.txt")) {
				return sub, nil
			}
		}
	}
	return "", fmt.Errorf("CMakeLists.txt not found")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// extractZip extracts a ZIP archive to destPath, guarding against zip-slip path
// traversal by verifying every entry stays within destPath.
func extractZip(zipPath, destPath string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	destAbs, err := filepath.Abs(destPath)
	if err != nil {
		return err
	}

	for _, f := range reader.File {
		target := filepath.Join(destPath, f.Name)
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		// zip-slip guard: the resolved path must be inside destPath.
		if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in archive (zip slip): %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := writeZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}
