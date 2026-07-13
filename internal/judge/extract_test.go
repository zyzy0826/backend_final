package judge

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeZip creates a ZIP at path whose entries use the exact names given (the
// caller controls the separators, so we can emit non-conforming backslash names).
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range entries {
		hw, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := hw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// Windows PowerShell's Compress-Archive stores nested entries with backslash
// separators; extractZip must recreate them as real subdirectories rather than
// a single file whose name literally contains a backslash.
func TestExtractZipNormalizesBackslashSeparators(t *testing.T) {
	if runtime.GOOS == "windows" {
		// On Windows the backslash is itself a path separator, so filepath would
		// split the entry into subdirectories regardless of the fix — the bug (and
		// thus this test) is only meaningful under Linux path semantics, which is
		// where the judge actually extracts submissions.
		t.Skip("backslash-separator handling is only observable on non-Windows")
	}

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	writeZip(t, zipPath, map[string]string{
		`CMakeLists.txt`:          "cmake_minimum_required(VERSION 3.30)\n",
		`cmake\AddJudge.cmake`:    "function(AddJudge)\nendfunction()\n",
		`spec\case1.h`:            "// case1\n",
	})

	dest := filepath.Join(dir, "out")
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	// The nested files must exist at their normalized paths.
	for _, rel := range []string{
		filepath.Join("cmake", "AddJudge.cmake"),
		filepath.Join("spec", "case1.h"),
		"CMakeLists.txt",
	} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}

	// The literal backslash name must NOT survive as a single file.
	if _, err := os.Stat(filepath.Join(dest, `cmake\AddJudge.cmake`)); err == nil {
		t.Errorf("backslash entry was not normalized; literal name still present")
	}
}

// The zip-slip guard must still reject traversal, including via backslashes.
func TestExtractZipRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	writeZip(t, zipPath, map[string]string{
		`..\..\escape.txt`: "pwned\n",
	})

	dest := filepath.Join(dir, "out")
	if err := extractZip(zipPath, dest); err == nil {
		t.Fatal("expected zip-slip to be rejected, got nil error")
	}
}
