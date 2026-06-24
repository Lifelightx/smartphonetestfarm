package utils_test

import (
	"os"
	"path/filepath"
	"testing"

	"protean-provider/internal/utils"
)

func TestInstallScrcpyServer_ValidJar(t *testing.T) {
	// Use the existing scrcpy-server.jar in the stream folder as the source.
	src := filepath.Join("..", "stream", "scrcpy-server.jar")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("scrcpy-server.jar not present, skipping: %v", err)
	}

	// Write to a temp destination so we don't overwrite the real file.
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "scrcpy-server.jar")

	// Temporarily redirect destinations by calling copyFile directly via the
	// public helper validateZipJar (package-internal copy tested implicitly).
	if err := utils.InstallScrcpyServerTo(src, dst); err != nil {
		t.Fatalf("InstallScrcpyServerTo: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
}

func TestInstallScrcpyServer_RejectsNonZip(t *testing.T) {
	// Create a fake file that is definitely not a ZIP.
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "not-a-jar")
	if err := os.WriteFile(fake, []byte("this is not a jar file"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := utils.InstallScrcpyServerTo(fake, filepath.Join(tmp, "out.jar")); err == nil {
		t.Fatal("expected error for non-ZIP file, got nil")
	}
}
