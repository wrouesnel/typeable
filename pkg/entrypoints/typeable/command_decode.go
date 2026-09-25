package typeable

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/wrouesnel/ctxstdio"
	"github.com/wrouesnel/typeable/pkg/codec"
	"github.com/wrouesnel/typeable/pkg/decodetui"
)

// ErrRowsMissing is returned when input ends before every row has been entered.
var ErrRowsMissing = errors.New("input ended before every row was entered")

// ErrCancelled is returned when the interactive editor is quit before decoding.
var ErrCancelled = errors.New("decoding cancelled")

// DecodeCmd reads a typed grid from stdin and writes the original data to stdout.
type DecodeCmd struct {
	Lines bool `help:"Read the grid line by line even when stdin is a terminal"`
}

// isTerminal reports whether r is an interactive terminal.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Run decodes stdin with the interactive editor if stdin is a terminal, and line by
// line otherwise.
func (c *DecodeCmd) Run(ctx context.Context) error {
	if !c.Lines && isTerminal(ctxstdio.Stdin(ctx)) {
		return c.runEditor(ctx)
	}
	return c.runLines(ctx)
}

// runEditor runs the interactive editor. It draws on stderr so stdout carries only the
// decoded data.
func (c *DecodeCmd) runEditor(ctx context.Context) error {
	model := decodetui.New()
	program := tea.NewProgram(model,
		tea.WithContext(ctx),
		tea.WithInput(ctxstdio.Stdin(ctx)),
		tea.WithOutput(ctxstdio.StdErr(ctx)))
	if _, err := program.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	plaintext := model.Plaintext()
	if plaintext == nil {
		return ErrCancelled
	}
	_, err := ctxstdio.StdOut(ctx).Write(plaintext)
	return err
}

// runLines decodes stdin line by line, reporting on each row as it is entered. It
// finishes as soon as every row has been entered.
func (c *DecodeCmd) runLines(ctx context.Context) error {
	stdIn := ctxstdio.Stdin(ctx)
	stdErr := ctxstdio.StdErr(ctx)

	// Read in the background so an interrupt isn't stuck behind a blocking read.
	lines := make(chan string)
	readErr := make(chan error, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdIn)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
	}()

	decoder := codec.NewDecoder()
	for !decoder.Complete() {
		var line string
		var ok bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok = <-lines:
		}
		if !ok {
			if err := <-readErr; err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			if !decoder.HaveHeader() {
				return fmt.Errorf("%w: no header line", ErrRowsMissing)
			}
			return fmt.Errorf("%w: missing rows %v", ErrRowsMissing, decoder.Missing())
		}

		result, err := decoder.ParseLine(line)
		if err != nil {
			_, _ = fmt.Fprintf(stdErr, "error: %v\n", err)
			continue
		}
		if result == nil {
			continue
		}
		for _, correction := range result.Corrections {
			_, _ = fmt.Fprintf(stdErr, "row %d: %v\n", result.Row, correction)
		}
	}

	plaintext, err := decoder.Plaintext()
	if err != nil {
		return err
	}
	_, err = ctxstdio.StdOut(ctx).Write(plaintext)
	return err
}
