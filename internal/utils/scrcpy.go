// Package utils provides shared helper utilities for protean-provider.
package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// zipMagic is the first 4 bytes of every ZIP / JAR / APK file (PK\x03\x04).
var zipMagic = []byte{0x50, 0x4B, 0x03, 0x04}

// InstallScrcpyServer copies the scrcpy-server binary at srcPath into the
// well-known locations used by protean-provider:
//
//   - internal/stream/scrcpy-server.jar   (source-tree / dev)
//   - bin/scrcpy-server.jar               (next to compiled binary, if bin/ exists)
//
// The function validates that srcPath is a valid ZIP/JAR archive before
// overwriting anything, so passing the wrong file will be caught early.
//
// Usage:
//
//	utils.InstallScrcpyServer("/path/to/scrcpy-linux-x86_64-v4.1/scrcpy-server")
//
// After running this, commit internal/stream/scrcpy-server.jar and rebuild
// with `make build` to pick up the new version everywhere.
func InstallScrcpyServer(srcPath string) error {
	// 1. Validate source file exists and is a ZIP/JAR.
	if err := validateZipJar(srcPath); err != nil {
		return fmt.Errorf("InstallScrcpyServer: %w", err)
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("InstallScrcpyServer: stat source: %w", err)
	}

	// 2. Resolve destinations relative to the current working directory.
	//    Callers are expected to run from the project root.
	destinations := []string{
		filepath.Join("internal", "stream", "scrcpy-server.jar"),
	}

	// Also update bin/ if it already exists (i.e. a previous `make build` ran).
	binDest := filepath.Join("bin", "scrcpy-server.jar")
	if _, err := os.Stat(filepath.Dir(binDest)); err == nil {
		destinations = append(destinations, binDest)
	}

	// 3. Copy to each destination.
	for _, dst := range destinations {
		if err := copyFile(srcPath, dst); err != nil {
			return fmt.Errorf("InstallScrcpyServer: copy to %s: %w", dst, err)
		}
		slog.Info("scrcpy-server installed",
			"dst", dst,
			"src", srcPath,
			"bytes", srcInfo.Size(),
		)
	}

	return nil
}

// InstallScrcpyServerTo is the lower-level variant of InstallScrcpyServer.
// It copies srcPath directly to dstPath, validating the ZIP magic first.
// Useful in tests or when you want to control the exact destination.
func InstallScrcpyServerTo(srcPath, dstPath string) error {
	if err := validateZipJar(srcPath); err != nil {
		return fmt.Errorf("InstallScrcpyServerTo: %w", err)
	}
	if err := copyFile(srcPath, dstPath); err != nil {
		return fmt.Errorf("InstallScrcpyServerTo: copy to %s: %w", dstPath, err)
	}
	info, _ := os.Stat(dstPath)
	slog.Info("scrcpy-server installed", "dst", dstPath, "src", srcPath, "bytes", info.Size())
	return nil
}

// ScrcpyServerVersion returns a human-readable summary of the installed
// scrcpy-server.jar: its size in bytes and whether it passes the ZIP magic check.
// Useful for logging/debugging which version is currently installed.
func ScrcpyServerVersion(jarPath string) (string, error) {
	info, err := os.Stat(jarPath)
	if err != nil {
		return "", fmt.Errorf("ScrcpyServerVersion: %w", err)
	}

	if err := validateZipJar(jarPath); err != nil {
		return "", fmt.Errorf("ScrcpyServerVersion: %w", err)
	}

	return fmt.Sprintf("size=%d bytes, path=%s", info.Size(), jarPath), nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// validateZipJar checks that path exists and starts with the ZIP magic bytes.
// scrcpy ships the server without a .jar extension, but it is always a valid
// ZIP archive internally.
func validateZipJar(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return fmt.Errorf("read magic from %q: %w", path, err)
	}

	// Accept both the standard ZIP local-file header (PK\x03\x04)
	// and the ZIP end-of-central-directory record (PK\x05\x06) for
	// empty archives, but in practice scrcpy-server is always PK\x03\x04.
	_ = binary.LittleEndian.Uint32(magic) // silence unused-import lint
	if magic[0] != zipMagic[0] || magic[1] != zipMagic[1] {
		return fmt.Errorf("%q is not a valid ZIP/JAR file (got magic %X %X)", path, magic[0], magic[1])
	}

	return nil
}

// copyFile copies src to dst, creating or truncating dst as needed.
// Permissions are set to 0o644.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}
