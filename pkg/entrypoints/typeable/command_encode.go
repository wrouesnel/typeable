package typeable

import (
	"context"
	"fmt"
	"io"

	"github.com/wrouesnel/ctxstdio"
	"github.com/wrouesnel/typeable/pkg/codec"
)

// EncodeCmd reads data from stdin and writes it to stdout as a grid to be retyped.
type EncodeCmd struct{}

// Run encodes stdin.
func (e *EncodeCmd) Run(ctx context.Context) error {
	plaintext, err := io.ReadAll(ctxstdio.Stdin(ctx))
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	grid, err := codec.Encode(plaintext)
	if err != nil {
		return err
	}
	return grid.Format(ctxstdio.StdOut(ctx))
}
