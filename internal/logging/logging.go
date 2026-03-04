package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	verbose atomic.Bool

	mu         sync.Mutex
	fileOutput io.Writer
	outputFile *os.File
	outputPath string
)

// SetVerbose enables or disables debug logging for the current process.
func SetVerbose(enabled bool) {
	verbose.Store(enabled)
}

// Verbose reports whether debug logging is enabled.
func Verbose() bool {
	return verbose.Load()
}

// SetOutputFile configures optional file logging while preserving stdout output.
// Passing an empty path disables file logging.
func SetOutputFile(path string) error {
	path = strings.TrimSpace(path)

	mu.Lock()
	defer mu.Unlock()

	if path == outputPath {
		return nil
	}

	if outputFile != nil {
		if err := outputFile.Close(); err != nil {
			outputFile = nil
			outputPath = ""
			fileOutput = nil
			return err
		}
		outputFile = nil
		outputPath = ""
	}

	fileOutput = nil
	if path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	outputFile = f
	outputPath = path
	fileOutput = f
	return nil
}

// Close flushes and closes the log file if one is configured.
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if outputFile == nil {
		return nil
	}
	err := outputFile.Close()
	outputFile = nil
	outputPath = ""
	fileOutput = nil
	return err
}

// Infof prints formatted output regardless of verbosity level.
func Infof(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(os.Stdout, format, args...)
	if fileOutput != nil {
		fmt.Fprintf(fileOutput, format, args...)
	}
}

// Infoln prints output regardless of verbosity level.
func Infoln(args ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintln(os.Stdout, args...)
	if fileOutput != nil {
		fmt.Fprintln(fileOutput, args...)
	}
}

// Debugf prints formatted output only when verbose mode is enabled.
func Debugf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if fileOutput != nil {
		fmt.Fprintf(fileOutput, format, args...)
	}
	if verbose.Load() {
		fmt.Fprintf(os.Stdout, format, args...)
	}
}
