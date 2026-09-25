package typeable_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/wrouesnel/ctxstdio"
	"github.com/wrouesnel/typeable/pkg/entrypoints/typeable"
	"github.com/wrouesnel/typeable/version"
)

// runEntrypoint invokes the entrypoint as though from the command line and captures its output.
func runEntrypoint(t *testing.T, stdIn string, args ...string) (int, string, string) {
	t.Helper()
	stdOut, stdErr := new(bytes.Buffer), new(bytes.Buffer)
	ctx := ctxstdio.Set(context.Background(), stdOut, stdErr, io.NopCloser(strings.NewReader(stdIn)))
	exitCode := typeable.Entrypoint(ctx, args)
	return exitCode, stdOut.String(), stdErr.String()
}

func TestVersion(t *testing.T) {
	exitCode, stdOut, _ := runEntrypoint(t, "", "--version")
	if exitCode != 0 {
		t.Fatalf("exit code: got %d, want 0", exitCode)
	}
	if !strings.Contains(stdOut, version.Version) {
		t.Errorf("stdout %q does not contain version %q", stdOut, version.Version)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	plaintext := "Some text\nto be typed back in.\n"
	exitCode, grid, _ := runEntrypoint(t, plaintext, "encode")
	if exitCode != 0 {
		t.Fatalf("encode exit code: got %d, want 0", exitCode)
	}
	exitCode, decoded, stdErr := runEntrypoint(t, grid, "decode")
	if exitCode != 0 {
		t.Fatalf("decode exit code: got %d, want 0: %s", exitCode, stdErr)
	}
	if decoded != plaintext {
		t.Errorf("got %q, want %q", decoded, plaintext)
	}
}

func TestDecodeReportsCorrections(t *testing.T) {
	_, grid, _ := runEntrypoint(t, "hello", "encode")
	lines := strings.Split(grid, "\n")
	// An unreadable character in the first data block.
	fields := strings.Fields(lines[1])
	fields[1] = "U" + fields[1][1:]
	lines[1] = strings.Join(fields, " ")

	exitCode, decoded, stdErr := runEntrypoint(t, strings.Join(lines, "\n"), "decode")
	if exitCode != 0 || decoded != "hello" {
		t.Fatalf("got exit %d, output %q: %s", exitCode, decoded, stdErr)
	}
	if !strings.Contains(stdErr, "row 1: column A character 1") {
		t.Errorf("stderr %q does not report the correction", stdErr)
	}
}

func TestDecodeMissingRowsFails(t *testing.T) {
	_, grid, _ := runEntrypoint(t, "hello", "encode")
	header := strings.SplitN(grid, "\n", 2)[0]
	exitCode, _, stdErr := runEntrypoint(t, header, "decode")
	if exitCode != 1 {
		t.Fatalf("exit code: got %d, want 1", exitCode)
	}
	// A plain message, not log output.
	if want := "typeable: input ended before every row was entered: missing rows [1]\n"; stdErr != want {
		t.Errorf("stderr: got %q, want %q", stdErr, want)
	}
}

func TestInvalidFlagFails(t *testing.T) {
	exitCode, _, stdErr := runEntrypoint(t, "", "--no-such-flag")
	if exitCode == 0 {
		t.Fatal("exit code: got 0, want non-zero")
	}
	if !strings.Contains(stdErr, "no-such-flag") {
		t.Errorf("stderr %q does not mention the bad flag", stdErr)
	}
}

func TestDecodeCancelledExitCode(t *testing.T) {
	// Input that never arrives, so decoding is still waiting when it is cancelled.
	stdIn, stdInWriter := io.Pipe()
	defer stdInWriter.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stdErr := new(bytes.Buffer)
	ctx = ctxstdio.Set(ctx, new(bytes.Buffer), stdErr, stdIn)

	exitCode := typeable.Entrypoint(ctx, []string{"decode"})
	if exitCode != typeable.ExitCancelled {
		t.Fatalf("exit code: got %d, want %d", exitCode, typeable.ExitCancelled)
	}
	if stdErr.Len() != 0 {
		t.Errorf("stderr: got %q, want nothing", stdErr.String())
	}
}
