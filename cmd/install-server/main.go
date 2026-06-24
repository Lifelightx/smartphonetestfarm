// install-server is a small helper CLI that copies a scrcpy-server binary
// (from any scrcpy release download) into the protean-provider source tree.
//
// Usage (run from project root):
//
//	go run ./cmd/install-server /path/to/scrcpy-server
//	go run ./cmd/install-server ~/Downloads/scrcpy-linux-x86_64-v4.1/scrcpy-server
//
// After running, commit the updated internal/stream/scrcpy-server.jar and
// rebuild with `make build` to pick up the new version.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"protean-provider/internal/utils"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: install-server <path-to-scrcpy-server>\n\n")
		fmt.Fprintf(os.Stderr, "  Example:\n")
		fmt.Fprintf(os.Stderr, "    go run ./cmd/install-server ~/test/scrcpy-linux-x86_64-v4.0/scrcpy-server\n\n")
		fmt.Fprintf(os.Stderr, "  This copies the binary to internal/stream/scrcpy-server.jar\n")
		fmt.Fprintf(os.Stderr, "  (and bin/scrcpy-server.jar if bin/ already exists).\n")
		os.Exit(1)
	}

	srcPath := os.Args[1]

	slog.Info("installing scrcpy-server", "src", srcPath)

	if err := utils.InstallScrcpyServer(srcPath); err != nil {
		slog.Error("failed to install scrcpy-server", "err", err)
		os.Exit(1)
	}

	slog.Info("done — commit internal/stream/scrcpy-server.jar and run `make build`")
}
