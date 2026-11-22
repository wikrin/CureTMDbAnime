package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestExecuteHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		result, handled, output, err := executeWithCapturedOutput(t, args, "test-version")
		if err != nil || !handled || result.ConfigFlags != nil {
			t.Fatalf("help should be handled successfully: handled=%v, error=%v", handled, err)
		}
		if !strings.Contains(output, "--port") || !strings.Contains(output, "8632") || !strings.Contains(output, "服务监听端口") {
			t.Fatalf("help lacks configuration metadata: %q", output)
		}
	}
	for _, version := range []string{"", "test-version"} {
		want := version
		if want == "" {
			want = "dev"
		}
		_, handled, output, err := executeWithCapturedOutput(t, []string{"--version"}, version)
		if err != nil || !handled || output != want+"\n" {
			t.Fatalf("version output=%q, handled=%v, error=%v", output, handled, err)
		}
	}
}

func TestExecuteConfigurationFlags(t *testing.T) {
	result, handled, _, err := executeWithCapturedOutput(t, []string{"--port=9000", "--debug"}, "dev")
	if err != nil || handled || result.ConfigFlags == nil {
		t.Fatalf("configuration parsing failed: handled=%v, error=%v", handled, err)
	}
	if port, err := result.ConfigFlags.GetInt("port"); err != nil || port != 9000 {
		t.Fatalf("port=%d, error=%v", port, err)
	}
	if debug, err := result.ConfigFlags.GetBool("debug"); err != nil || !debug {
		t.Fatalf("debug=%v, error=%v", debug, err)
	}
	_, _, _, err = executeWithCapturedOutput(t, []string{"--port=invalid"}, "dev")
	if err == nil {
		t.Fatal("invalid configuration flag should return an error")
	}
}

func executeWithCapturedOutput(t *testing.T, args []string, version string) (Result, bool, string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	result, handled, executeErr := Execute(args, version)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return result, handled, string(output), executeErr
}
