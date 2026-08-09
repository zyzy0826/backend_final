package judge

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"regs/internal/config"
	"regs/internal/model"
	"regs/internal/repository"
)

type Judge struct {
	runner      *DockerRunner
	subRepo     *repository.SubmissionRepository
	problemRepo *repository.ProblemRepository
	cfg         *config.Config
}

func New(runner *DockerRunner, subRepo *repository.SubmissionRepository, problemRepo *repository.ProblemRepository, cfg *config.Config) *Judge {
	return &Judge{runner: runner, subRepo: subRepo, problemRepo: problemRepo, cfg: cfg}
}

// JobInput carries only identifiers; problem data (package / testcases / time
// limit) is loaded from the DB when the job runs, so a rejudge always picks up
// the latest problem definition.
type JobInput struct {
	SubmissionID int
	OperatorID   string
	ProblemID    int
	ZipPath      string
}

const (
	configureTimeout = 120 // seconds for `cmake -G`
	compileTimeout   = 300 // seconds for `cmake --build`
	listTimeout      = 30  // seconds for `ctest --show-only`
)

const (
	// workspaceMountPoint is where a single submission's workspace appears
	// inside a judge container.
	workspaceMountPoint = "/workspace"
	// storageMountPoint is where the whole storage volume appears when the
	// daemon is too old to mount just one directory out of it.
	storageMountPoint = "/storage"
)

// RunJob executes the full judge pipeline for a single submission.
//
// Test-based mode (problem has an uploaded package ZIP):
//
//	Phase1: cmake -G Ninja -S <problem> -B build -D SOURCE_ROOT=<student src>  (SE on fail)
//	Phase2: cmake --build build            (CE on fail)
//	Phase3: run each ctest case executable (network=none, AC/WA/RE/TLE per case)
//
// I/O mode (no package; testcases stored in DB):
//
//	Phase1: cmake -G Ninja -S <student project> -B build  (SE on fail)
//	Phase2: cmake --build build                           (CE on fail)
//	Phase3: run the built binary per testcase, compare stdout (AC/WA/RE/TLE)
func (j *Judge) RunJob(ctx context.Context, job JobInput) {
	if err := j.subRepo.UpdateStatus(ctx, job.SubmissionID, model.StatusRunning); err != nil {
		log.Printf("judge: mark running for submission %d: %v", job.SubmissionID, err)
	}

	problem, err := j.problemRepo.FindByID(ctx, job.ProblemID)
	if err != nil {
		j.done(ctx, job, model.StatusSE, "failed to load problem: "+err.Error(), "", "")
		return
	}
	timeLimit := problem.TimeLimit
	if timeLimit <= 0 {
		timeLimit = j.cfg.TimeLimitSeconds
	}

	// Per-submission workspace. The uploaded ZIP is unpacked into src/ at upload
	// time and both the archive and the extracted files are kept afterwards, so
	// the workspace is intentionally NOT removed when the job finishes. Only the
	// artifacts of a previous run (build/, problem/) are wiped so a rejudge
	// starts from a clean build while reusing the already-extracted sources.
	workspace := workspaceDir(j.cfg, job.OperatorID)
	srcDir := filepath.Join(workspace, "src")
	os.RemoveAll(filepath.Join(workspace, "build"))
	os.RemoveAll(filepath.Join(workspace, "problem"))

	// Fallback: if the sources are not present (e.g. a rejudge after the
	// workspace was cleaned manually), re-extract them from the stored ZIP.
	if !dirHasEntries(srcDir) {
		if err := os.MkdirAll(srcDir, 0755); err != nil {
			j.done(ctx, job, model.StatusSE, "failed to create workspace: "+err.Error(), "", "")
			return
		}
		if err := extractZip(job.ZipPath, srcDir); err != nil {
			j.done(ctx, job, model.StatusSE, "failed to extract submission ZIP: "+err.Error(), "", "")
			return
		}
	}

	mnt := j.workspaceMount(workspace)

	if problem.PackagePath != "" {
		j.runTestBased(ctx, job, problem, workspace, mnt, srcDir, timeLimit)
		return
	}
	j.runIOMode(ctx, job, workspace, mnt, srcDir, timeLimit)
}

// --- Test-based mode -------------------------------------------------------

func (j *Judge) runTestBased(ctx context.Context, job JobInput, problem *model.Problem, workspace string, mnt Mount, srcDir string, timeLimit int) {
	problemDir := filepath.Join(workspace, "problem")
	if err := extractZip(problem.PackagePath, problemDir); err != nil {
		j.done(ctx, job, model.StatusSE, "failed to extract problem package: "+err.Error(), "", "")
		return
	}
	problemRoot, err := findCMakeRoot(problemDir)
	if err != nil {
		j.done(ctx, job, model.StatusSE, "problem package has no CMakeLists.txt", "", "")
		return
	}

	problemC := containerPath(workspace, mnt.Dir, problemRoot)
	srcC := containerPath(workspace, mnt.Dir, findSourceRoot(srcDir))
	buildC := mnt.Dir + "/build"

	// --- Phase 1: configure against the problem's CMake project, pointing
	// SOURCE_ROOT at the student's uploaded sources. ---
	r1, err := j.runner.Run(ctx, mnt,
		[]string{"cmake", "-G", "Ninja", "-S", problemC, "-B", buildC, "-D", "SOURCE_ROOT=" + srcC},
		"bridge", configureTimeout, false)
	configureLog := logFrom(r1)
	if err != nil {
		configureLog += "\n[runner error] " + err.Error()
	}
	if err != nil || r1 == nil || r1.ExitCode != 0 {
		j.done(ctx, job, model.StatusSE, configureLog, "", "")
		return
	}

	// --- Phase 2: build all case targets. ---
	r2, err := j.runner.Run(ctx, mnt,
		[]string{"cmake", "--build", buildC, "--verbose"}, "bridge", compileTimeout, false)
	compileLog := logFrom(r2)
	if err != nil {
		compileLog += "\n[runner error] " + err.Error()
	}
	if err != nil || r2 == nil || r2.ExitCode != 0 {
		j.done(ctx, job, model.StatusCE, configureLog, compileLog, "")
		return
	}

	// --- Phase 3: discover the registered ctest cases, then run every case in
	// its own fully network-isolated container. ---
	cases, listLog, err := j.listCases(ctx, mnt, buildC)
	if err != nil {
		j.done(ctx, job, model.StatusSE, configureLog, compileLog, "failed to list test cases:\n"+listLog+"\n"+err.Error())
		return
	}

	finalStatus := model.StatusAC
	var outputLog strings.Builder
	if len(cases) == 0 {
		outputLog.WriteString("[warning] no test cases registered; treated as AC after successful build\n")
	}

	passed := 0
	for _, tc := range cases {
		outputLog.WriteString(fmt.Sprintf("=== %s ===\n", tc.Name))

		r, err := j.runner.Run(ctx, mnt, tc.Command, "none", timeLimit, true)
		outputLog.WriteString(logFrom(r))

		caseStatus := model.StatusAC
		switch {
		case r != nil && r.TimedOut:
			caseStatus = model.StatusTLE
			outputLog.WriteString("\n[TLE] execution exceeded time limit\n")
		case err != nil:
			caseStatus = model.StatusRE
			outputLog.WriteString("\n[runner error] " + err.Error() + "\n")
		case r.ExitCode == 0:
			passed++
			outputLog.WriteString("\n[PASS]\n")
		case r.ExitCode >= 125:
			// 125-127: docker/exec failures, 128+n: killed by signal (e.g. segfault).
			caseStatus = model.StatusRE
			outputLog.WriteString(fmt.Sprintf("\n[RE] abnormal termination, exit code: %d\n", r.ExitCode))
		default:
			// Test framework reported failed assertions.
			caseStatus = model.StatusWA
			outputLog.WriteString(fmt.Sprintf("\n[FAIL] test case failed, exit code: %d\n", r.ExitCode))
		}

		// All cases run to completion for full feedback; the first failure
		// decides the overall status.
		if caseStatus != model.StatusAC && finalStatus == model.StatusAC {
			finalStatus = caseStatus
		}
	}
	if len(cases) > 0 {
		outputLog.WriteString(fmt.Sprintf("\n=== Summary: %d/%d cases passed ===\n", passed, len(cases)))
	}

	j.done(ctx, job, finalStatus, configureLog, compileLog, outputLog.String())
}

type ctestCase struct {
	Name    string
	Command []string
}

// listCases asks ctest for the registered tests (json-v1) without running them.
func (j *Judge) listCases(ctx context.Context, mnt Mount, buildDir string) ([]ctestCase, string, error) {
	r, err := j.runner.Run(ctx, mnt,
		[]string{"ctest", "--test-dir", buildDir, "--show-only=json-v1"},
		"none", listTimeout, false)
	if err != nil {
		return nil, logFrom(r), err
	}
	if r.ExitCode != 0 {
		return nil, logFrom(r), fmt.Errorf("ctest --show-only exited with code %d", r.ExitCode)
	}

	var doc struct {
		Tests []struct {
			Name    string   `json:"name"`
			Command []string `json:"command"`
		} `json:"tests"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &doc); err != nil {
		return nil, r.Stdout, fmt.Errorf("parse ctest json: %w", err)
	}

	cases := make([]ctestCase, 0, len(doc.Tests))
	for _, t := range doc.Tests {
		if len(t.Command) == 0 {
			continue
		}
		cases = append(cases, ctestCase{Name: t.Name, Command: t.Command})
	}
	return cases, "", nil
}

// --- I/O mode ---------------------------------------------------------------

// runScript locates the first freshly-built executable under build/ and runs it
// with stdin redirected from the current testcase input.
func runScript(workspaceDir string) string {
	return `exe=$(find ` + workspaceDir + `/build -maxdepth 4 -type f -perm -u+x ` +
		`-not -path '*/CMakeFiles/*' -not -name '*.sh' -not -name '*.cmake' -not -name '*.so' | head -n1); ` +
		`if [ -z "$exe" ]; then echo "no executable found in build/" >&2; exit 127; fi; ` +
		`"$exe" < ` + workspaceDir + `/input.txt`
}

func (j *Judge) runIOMode(ctx context.Context, job JobInput, workspace string, mnt Mount, srcDir string, timeLimit int) {
	// I/O mode judges the student's own CMake project, which must ship a
	// CMakeLists.txt (either at the archive root or in a single top folder).
	projectRoot, err := findCMakeRoot(srcDir)
	if err != nil {
		j.done(ctx, job, model.StatusSE, "CMakeLists.txt not found in project root", "", "")
		return
	}
	projectC := containerPath(workspace, mnt.Dir, projectRoot)
	buildC := mnt.Dir + "/build"

	// --- Phase 1: configure (cmake -G Ninja) ---
	r1, err := j.runner.Run(ctx, mnt,
		[]string{"cmake", "-G", "Ninja", "-S", projectC, "-B", buildC},
		"bridge", configureTimeout, false)
	configureLog := logFrom(r1)
	if err != nil {
		configureLog += "\n[runner error] " + err.Error()
	}
	if err != nil || r1 == nil || r1.ExitCode != 0 {
		j.done(ctx, job, model.StatusSE, configureLog, "", "")
		return
	}

	// --- Phase 2: build (cmake --build --verbose) ---
	r2, err := j.runner.Run(ctx, mnt,
		[]string{"cmake", "--build", buildC, "--verbose"}, "bridge", compileTimeout, false)
	compileLog := logFrom(r2)
	if err != nil {
		compileLog += "\n[runner error] " + err.Error()
	}
	if err != nil || r2 == nil || r2.ExitCode != 0 {
		j.done(ctx, job, model.StatusCE, configureLog, compileLog, "")
		return
	}

	// --- Phase 3: run each DB testcase in a network-isolated container ---
	testcases, err := j.problemRepo.GetTestcases(ctx, job.ProblemID)
	if err != nil {
		j.done(ctx, job, model.StatusSE, configureLog, compileLog, "failed to load testcases: "+err.Error())
		return
	}

	finalStatus := model.StatusAC
	var outputLog strings.Builder
	if len(testcases) == 0 {
		outputLog.WriteString("[warning] problem has no testcases; treated as AC after successful build\n")
	}

	passed := 0
	for i, tc := range testcases {
		outputLog.WriteString(fmt.Sprintf("=== Testcase %d ===\n", i+1))

		inputPath := filepath.Join(workspace, "input.txt")
		if err := os.WriteFile(inputPath, []byte(tc.Input), 0644); err != nil {
			finalStatus = model.StatusRE
			outputLog.WriteString("failed to write input: " + err.Error() + "\n")
			break
		}

		r, err := j.runner.Run(ctx, mnt,
			[]string{"sh", "-c", runScript(mnt.Dir)}, "none", timeLimit, true)

		caseStatus := model.StatusAC
		switch {
		case r != nil && r.TimedOut:
			caseStatus = model.StatusTLE
			outputLog.WriteString(logFrom(r))
			outputLog.WriteString("\n[TLE] execution exceeded time limit\n")
		case err != nil:
			caseStatus = model.StatusRE
			outputLog.WriteString("\n[runner error] " + err.Error() + "\n")
		case r.ExitCode != 0:
			caseStatus = model.StatusRE
			outputLog.WriteString(logFrom(r))
			outputLog.WriteString(fmt.Sprintf("\n[RE] non-zero exit code: %d\n", r.ExitCode))
		case !matchOutput(r.Stdout, tc.Expected):
			caseStatus = model.StatusWA
			outputLog.WriteString(fmt.Sprintf("[WA]\n--- expected ---\n%s\n--- got ---\n%s\n", tc.Expected, r.Stdout))
		default:
			passed++
			outputLog.WriteString("[AC]\n")
		}

		if caseStatus != model.StatusAC && finalStatus == model.StatusAC {
			finalStatus = caseStatus
		}
	}
	if len(testcases) > 0 {
		outputLog.WriteString(fmt.Sprintf("\n=== Summary: %d/%d testcases passed ===\n", passed, len(testcases)))
	}

	j.done(ctx, job, finalStatus, configureLog, compileLog, outputLog.String())
}

// --- Shared helpers ----------------------------------------------------------

// workspaceDir returns the per-submission workspace directory under StoragePath.
func workspaceDir(cfg *config.Config, operatorID string) string {
	return filepath.Join(cfg.StoragePath, "workspace", operatorID)
}

// PrepareWorkspace unpacks the uploaded ZIP into the submission workspace's src/
// directory. It is called at upload time so the extracted sources are available
// in the workspace before (and independently of) judging. Any previously
// extracted sources are replaced.
func PrepareWorkspace(cfg *config.Config, operatorID, zipPath string) error {
	srcDir := filepath.Join(workspaceDir(cfg, operatorID), "src")
	os.RemoveAll(srcDir)
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return err
	}
	if err := extractZip(zipPath, srcDir); err != nil {
		// Leave no partial extraction behind, so the judge's fallback re-extracts.
		os.RemoveAll(srcDir)
		return err
	}
	return nil
}

// dirHasEntries reports whether dir exists and contains at least one entry.
func dirHasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// done persists the final status plus logs to the DB, and also writes the three
// log segments as physical files under storage/logs/{operatorId}/.
func (j *Judge) done(ctx context.Context, job JobInput, status model.Status, configureLog, compileLog, outputLog string) {
	j.writeLogFiles(job.OperatorID, configureLog, compileLog, outputLog)
	if err := j.subRepo.UpdateStatusWithLogs(ctx, job.SubmissionID, status, configureLog, compileLog, outputLog); err != nil {
		log.Printf("judge: failed to persist result for submission %d: %v", job.SubmissionID, err)
	}
}

func (j *Judge) writeLogFiles(operatorID, configureLog, compileLog, outputLog string) {
	dir := filepath.Join(j.cfg.StoragePath, "logs", operatorID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("judge: mkdir log dir for %s: %v", operatorID, err)
		return
	}
	for name, content := range map[string]string{
		"configure.log": configureLog,
		"compile.log":   compileLog,
		"output.log":    outputLog,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			log.Printf("judge: write %s for %s: %v", name, operatorID, err)
		}
	}
}

// workspaceMount decides how a submission's workspace is handed to its judge
// containers. There are two ways to get the app and the judge containers to
// look at the same files:
//
// Volume mode (StorageVolume set — the containerised deployment): the judge
// container mounts the very same named volume that backs the app's StoragePath.
// The daemon resolves the volume by name, so no host path is involved and the
// stack behaves identically on Windows, macOS and Linux. On Docker Engine 25+
// only the submission's own directory is mounted (volume-subpath); on older
// daemons the whole volume has to be mounted instead.
//
// Bind mode (StorageVolume empty — server running directly on the host): the
// workspace directory is bind-mounted, which requires its path as the Docker
// daemon sees it, i.e. HostStoragePath.
func (j *Judge) workspaceMount(workspace string) Mount {
	rel, err := filepath.Rel(j.cfg.StoragePath, workspace)
	if err != nil {
		rel = ""
	}

	if vol := j.cfg.StorageVolume; vol != "" {
		if j.runner.SupportsVolumeSubpath() && rel != "" && rel != "." {
			spec := fmt.Sprintf("type=volume,source=%s,target=%s,volume-subpath=%s",
				vol, workspaceMountPoint, filepath.ToSlash(rel))
			return Mount{Args: []string{"--mount", spec}, Dir: workspaceMountPoint}
		}
		dir := storageMountPoint
		if rel != "" && rel != "." {
			dir += "/" + filepath.ToSlash(rel)
		}
		return Mount{Args: []string{"-v", vol + ":" + storageMountPoint}, Dir: dir}
	}

	host := filepath.Join(j.cfg.HostStoragePath, rel)
	if abs, err := filepath.Abs(host); err == nil {
		host = abs
	}
	if runtime.GOOS == "windows" {
		host = toDockerPath(host)
	}
	return Mount{Args: []string{"-v", host + ":" + workspaceMountPoint}, Dir: workspaceMountPoint}
}

// containerPath maps a local path inside the workspace to its path inside the
// container, where the workspace is rooted at containerWorkspace.
func containerPath(workspace, containerWorkspace, local string) string {
	rel, err := filepath.Rel(workspace, local)
	if err != nil || rel == "." {
		return containerWorkspace
	}
	return containerWorkspace + "/" + filepath.ToSlash(rel)
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

// findCMakeRoot returns the directory containing CMakeLists.txt: either the given
// directory itself, or a single top-level subdirectory (archives that wrap
// everything in a folder).
func findCMakeRoot(dir string) (string, error) {
	if fileExists(filepath.Join(dir, "CMakeLists.txt")) {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			sub := filepath.Join(dir, e.Name())
			if fileExists(filepath.Join(sub, "CMakeLists.txt")) {
				return sub, nil
			}
		}
	}
	return "", fmt.Errorf("CMakeLists.txt not found")
}

// findSourceRoot locates the student source root for test-based judging: the
// first directory that directly contains C/C++ sources, descending through
// single-folder wrappers created by archiving a directory.
func findSourceRoot(dir string) string {
	if hasCXXSources(dir) {
		return dir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir
	}
	var subdirs []string
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, filepath.Join(dir, e.Name()))
		}
	}
	if len(subdirs) == 1 {
		return findSourceRoot(subdirs[0])
	}
	return dir
}

func hasCXXSources(dir string) bool {
	for _, pat := range []string{"*.cpp", "*.cc", "*.cxx", "*.c"} {
		if matches, _ := filepath.Glob(filepath.Join(dir, pat)); len(matches) > 0 {
			return true
		}
	}
	return false
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
		// Windows PowerShell's Compress-Archive writes entry names with backslash
		// separators, which violate the ZIP spec (forward slash only). On Linux a
		// backslash is an ordinary filename character, so "cmake\AddJudge.cmake"
		// would extract as a single oddly-named file at the root instead of
		// cmake/AddJudge.cmake, collapsing every nested directory. Normalize first.
		name := strings.ReplaceAll(f.Name, "\\", "/")
		target := filepath.Join(destPath, name)
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
