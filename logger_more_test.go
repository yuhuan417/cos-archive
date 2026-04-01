package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDebugCollector(t *testing.T) {
	dc := NewDebugCollector()
	if dc.HasContent() {
		t.Fatal("new collector should be empty")
	}
	if _, err := dc.Write([]byte("debug message")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !dc.HasContent() || dc.GetContent() != "debug message" {
		t.Fatalf("unexpected collector content: %q", dc.GetContent())
	}
	dc.Clear()
	if dc.HasContent() {
		t.Fatal("collector should be empty after clear")
	}
}

func TestSimpleAndCustomHandlers(t *testing.T) {
	infoBuf := &strings.Builder{}
	debugBuf := &strings.Builder{}

	h := &SimpleHandler{writer: infoBuf}
	record := slog.NewRecord(time.Unix(1700000000, 0), slog.LevelWarn, "warn-msg", 0)
	record.AddAttrs(slog.String("key", "value"))
	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("simple handler should enable debug level")
	}
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := infoBuf.String(); !strings.Contains(got, "W warn-msg key=value") {
		t.Fatalf("unexpected simple handler output: %q", got)
	}
	if h.WithAttrs(nil) != h || h.WithGroup("group") != h {
		t.Fatal("simple handler WithAttrs/WithGroup should return receiver")
	}

	custom := &CustomHandler{
		infoHandler:  &SimpleHandler{writer: infoBuf},
		debugHandler: &SimpleHandler{writer: debugBuf},
	}
	if !custom.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("custom handler should enable all levels")
	}
	debugRecord := slog.NewRecord(time.Unix(1700000001, 0), slog.LevelDebug, "debug-msg", 0)
	if err := custom.Handle(context.Background(), debugRecord); err != nil {
		t.Fatalf("custom handle debug: %v", err)
	}
	infoRecord := slog.NewRecord(time.Unix(1700000002, 0), slog.LevelInfo, "info-msg", 0)
	if err := custom.Handle(context.Background(), infoRecord); err != nil {
		t.Fatalf("custom handle info: %v", err)
	}
	if !strings.Contains(debugBuf.String(), "debug-msg") || !strings.Contains(infoBuf.String(), "info-msg") {
		t.Fatalf("unexpected custom handler routing: info=%q debug=%q", infoBuf.String(), debugBuf.String())
	}
	if custom.WithAttrs(nil) == custom || custom.WithGroup("g") == custom {
		t.Fatal("custom handler WithAttrs/WithGroup should return new handlers")
	}
}

func TestInitLoggerAndPrintDebugDetails(t *testing.T) {
	output := captureStdout(t, func() {
		InitLogger(false)
		slog.Debug("debug-hidden")
		slog.Info("info-visible")
		PrintDebugDetails()
	})
	if !strings.Contains(output, "info-visible") {
		t.Fatalf("expected info log in stdout, got %q", output)
	}
	if !strings.Contains(output, "=== Debug Details ===") || !strings.Contains(output, "debug-hidden") {
		t.Fatalf("expected debug details in stdout, got %q", output)
	}

	verboseOutput := captureStdout(t, func() {
		InitLogger(true)
		slog.Debug("debug-visible")
		PrintDebugDetails()
	})
	if !strings.Contains(verboseOutput, "debug-visible") {
		t.Fatalf("expected verbose debug log in stdout, got %q", verboseOutput)
	}
	if strings.Contains(verboseOutput, "=== Debug Details ===") {
		t.Fatalf("did not expect debug details banner in verbose mode, got %q", verboseOutput)
	}
}

func TestFatalExits(t *testing.T) {
	if os.Getenv("TEST_FATAL_HELPER") == "1" {
		InitLogger(false)
		Fatal("fatal message")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalExits$")
	cmd.Env = append(os.Environ(), "TEST_FATAL_HELPER=1")
	out, err := cmd.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, err=%v output=%s", err, out)
	}
	if !strings.Contains(string(out), "fatal message") {
		t.Fatalf("expected fatal message in output, got %q", out)
	}
}

func TestFatalfExits(t *testing.T) {
	if os.Getenv("TEST_FATALF_HELPER") == "1" {
		InitLogger(false)
		Fatalf("fatal %s", "formatted")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalfExits$")
	cmd.Env = append(os.Environ(), "TEST_FATALF_HELPER=1")
	out, err := cmd.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, err=%v output=%s", err, out)
	}
	if !strings.Contains(string(out), "fatal formatted") {
		t.Fatalf("expected fatal formatted message in output, got %q", out)
	}
}
