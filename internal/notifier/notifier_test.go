package notifier_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ko5tas/t212/internal/notifier"
)

// fakeSignalCLI writes a shell script that records its arguments to a file.
func fakeSignalCLI(t *testing.T) (binPath, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("signal-cli fake not supported on Windows")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	binPath = filepath.Join(dir, "signal-cli")

	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}
	return binPath, argsFile
}

func TestNotifier_NotifyEntered(t *testing.T) {
	binPath, argsFile := fakeSignalCLI(t)

	n := notifier.New(binPath, "+447700000000")
	n.Notify("AAPL_US_EQ", true, 9.30)

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := string(data)

	if !strings.Contains(args, "send") {
		t.Errorf("expected 'send' in args, got: %q", args)
	}
	if !strings.Contains(args, "+447700000000") {
		t.Errorf("expected recipient number in args, got: %q", args)
	}
	if !strings.Contains(args, "AAPL_US_EQ") {
		t.Errorf("expected ticker in message, got: %q", args)
	}
}

func TestNotifier_NotifyExited(t *testing.T) {
	binPath, argsFile := fakeSignalCLI(t)

	n := notifier.New(binPath, "+447700000000")
	n.Notify("TSLA_US_EQ", false, 0)

	data, _ := os.ReadFile(argsFile)
	args := string(data)

	if !strings.Contains(args, "TSLA_US_EQ") {
		t.Errorf("expected ticker in message, got: %q", args)
	}
}

func TestNotifier_SignalCLINotFound(t *testing.T) {
	n := notifier.New("/nonexistent/signal-cli", "+447700000000")
	// Must not panic — just log the error.
	n.Notify("AAPL_US_EQ", true, 9.30)
}

func TestNotifier_ImplementsPollerNotifier(t *testing.T) {
	// Compile-time interface satisfaction check.
	var _ interface {
		Notify(ticker string, entered bool, profitPerShare float64)
	} = (*notifier.Notifier)(nil)
}
