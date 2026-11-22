package logger

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"curetmdbanime/internal/config"
)

func TestAllEmittedLevelsPersistWithoutBootstrapHistory(t *testing.T) {
	oldLevel := currentLogLevel.Load()
	oldInfo, oldDebug := AppLogger.infoLogger.Writer(), AppLogger.debugLogger.Writer()
	defer func() {
		currentLogLevel.Store(oldLevel)
		AppLogger.infoLogger.SetOutput(oldInfo)
		AppLogger.debugLogger.SetOutput(oldDebug)
	}()
	var info, debug bytes.Buffer
	AppLogger.infoLogger.SetOutput(&info)
	AppLogger.debugLogger.SetOutput(&debug)
	Info("before initialization")
	dataDir := t.TempDir()
	if err := Initialize(dataDir, 1, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	if err := Initialize(t.TempDir(), 1, 2); err == nil {
		t.Fatal("duplicate initialization accepted")
	}
	SetDebug(false)
	Debug("disabled debug")
	Info("persist info")
	Warn("persist warn")
	Error("persist error")
	SetDebug(true)
	Debug("persist debug")
	result, err := Query(context.Background(), QueryOptions{Limit: 10})
	if err != nil || len(result.Items) != 4 {
		t.Fatalf("%+v %v", result, err)
	}
	for i, level := range []string{"info", "warn", "error", "debug"} {
		if result.Items[i].Level != level {
			t.Fatal(result.Items)
		}
	}
	if strings.Contains(debug.String(), "disabled debug") || !strings.Contains(info.String(), "logger_test.go:") || !strings.Contains(info.String(), "before initialization") {
		t.Fatalf("console output changed: info=%q debug=%q", info.String(), debug.String())
	}
}

func TestFatalPersistsBeforeExit(t *testing.T) {
	if os.Getenv("CURE_LOGGER_FATAL_HELPER") == "1" {
		if err := Initialize(os.Getenv("CURE_LOGGER_FATAL_DIR"), 1, 1); err != nil {
			os.Exit(2)
		}
		Fatal("fatal persisted token=must-be-redacted")
		return
	}
	dataDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFatalPersistsBeforeExit$")
	command.Env = append(os.Environ(), "CURE_LOGGER_FATAL_HELPER=1", "CURE_LOGGER_FATAL_DIR="+dataDir)
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("unexpected fatal exit: %v %s", err, output)
	}
	files, err := filepath.Glob(filepath.Join(dataDir, "logs", "*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	entry, err := parseEntry(data)
	if err != nil || entry.Level != "fatal" || entry.ID != 1 || !strings.Contains(entry.Message, "[REDACTED]") || strings.Contains(entry.Message, "must-be-redacted") {
		t.Fatalf("fatal did not persist: %s", data)
	}
}

func TestDebugLevelAfterConfigLoad(t *testing.T) {
	oldSettings, oldLevel := config.AppSettings, currentLogLevel.Load()
	oldOutput := AppLogger.debugLogger.Writer()
	defer func() {
		config.AppSettings = oldSettings
		currentLogLevel.Store(oldLevel)
		AppLogger.debugLogger.SetOutput(oldOutput)
	}()
	var output bytes.Buffer
	AppLogger.debugLogger.SetOutput(&output)

	for _, enabled := range []bool{true, false} {
		flags := config.NewFlagSet()
		value := "false"
		if enabled {
			value = "true"
		}
		if err := flags.Parse([]string{"--debug=" + value}); err != nil {
			t.Fatal(err)
		}
		if err := config.LoadConfig(flags); err != nil {
			t.Fatal(err)
		}
		SetDebug(config.AppSettings.Debug)
		output.Reset()
		Debug("debug-after-config-load")
		if logged := strings.Contains(output.String(), "debug-after-config-load"); logged != enabled {
			t.Fatalf("debug=%v: unexpected output %q", enabled, output.String())
		}
	}
}
