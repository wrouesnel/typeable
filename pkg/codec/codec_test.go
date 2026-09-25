package codec_test

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/wrouesnel/typeable/pkg/codec"
)

// sampleText is long enough to need several rows, including a short last row.
const sampleText = `The quick brown fox jumps over the lazy dog.
Pack my box with five dozen liquor jugs, then 0123456789 and some punctuation!?
`

// randomBytes don't compress, so they give a larger grid.
func randomBytes(n int) []byte {
	r := rand.New(rand.NewPCG(7, 8))
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(r.IntN(256))
	}
	return data
}

func formatGrid(t *testing.T, plaintext []byte) []string {
	t.Helper()
	grid, err := codec.Encode(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := grid.Format(&buf); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

// decodeLines feeds lines to a decoder and returns the plaintext and all corrections.
func decodeLines(t *testing.T, lines []string) ([]byte, []codec.Correction) {
	t.Helper()
	d := codec.NewDecoder()
	var corrections []codec.Correction
	for _, line := range lines {
		result, err := d.ParseLine(line)
		if err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		if result != nil {
			corrections = append(corrections, result.Corrections...)
		}
	}
	if !d.Complete() {
		t.Fatalf("decoder incomplete, missing rows %v", d.Missing())
	}
	plaintext, err := d.Plaintext()
	if err != nil {
		t.Fatal(err)
	}
	return plaintext, corrections
}

// replaceChar replaces the character at a column and 1-based offset of a formatted row.
func replaceChar(t *testing.T, line string, column byte, offset int, with byte) string {
	t.Helper()
	fields := strings.Fields(line)
	idx := 1 + strings.IndexByte(codec.Columns, column)
	if column == codec.Columns[len(codec.Columns)-1] {
		idx = len(fields) - 1
	}
	block := []byte(fields[idx])
	if strings.EqualFold(string(block[offset-1]), string(with)) {
		t.Fatalf("test bug: %c already at %c%d", with, column, offset)
	}
	block[offset-1] = with
	fields[idx] = string(block)
	return strings.Join(fields, " ")
}

func TestRoundTrip(t *testing.T) {
	for _, plaintext := range [][]byte{{}, []byte("x"), []byte(sampleText), randomBytes(500)} {
		lines := formatGrid(t, plaintext)
		got, corrections := decodeLines(t, lines)
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("got %q, want %q", got, plaintext)
		}
		if len(corrections) != 0 {
			t.Errorf("unexpected corrections %v", corrections)
		}
	}
}

func TestLayout(t *testing.T) {
	lines := formatGrid(t, randomBytes(100))
	header := strings.Fields(lines[0])
	if strings.Join(header[1:], "") != codec.Columns {
		t.Errorf("header %q does not list the columns", lines[0])
	}
	for i, line := range lines[1 : len(lines)-1] {
		fields := strings.Fields(line)
		if len(fields) != 1+len(codec.Columns) {
			t.Errorf("row %d has %d fields: %q", i+1, len(fields), line)
		}
		for _, block := range fields[1:] {
			if len(block) != codec.BlockSize {
				t.Errorf("row %d block %q has the wrong size", i+1, block)
			}
			if block != strings.ToLower(block) {
				t.Errorf("row %d block %q is not lowercase", i+1, block)
			}
		}
	}
	// The parity block of a short last row still lines up with column H.
	last := lines[len(lines)-1]
	if len(last) != len(lines[1]) {
		t.Errorf("last row %q is not aligned with %q", last, lines[1])
	}
}

func TestRepairsMistakes(t *testing.T) {
	lines := formatGrid(t, randomBytes(100))
	// A typo in data, one in parity, and lowercase / O-for-0 entry of another row.
	lines[1] = replaceChar(t, lines[1], 'B', 3, 'U')
	lines[2] = replaceChar(t, lines[2], 'H', 5, 'Z')
	lines[3] = strings.ReplaceAll(strings.ToLower(lines[3]), "0", "o")

	_, corrections := decodeLines(t, lines)
	if len(corrections) != 2 {
		t.Fatalf("got corrections %v, want 2", corrections)
	}
	if c := corrections[0]; c.Column != 'B' || c.Offset != 3 || c.Typed != 'U' {
		t.Errorf("first correction %v", c)
	}
	if c := corrections[1]; c.Column != 'H' || c.Offset != 5 {
		t.Errorf("second correction %v", c)
	}
}

func TestRepairsTransposition(t *testing.T) {
	lines := formatGrid(t, randomBytes(100))
	fields := strings.Fields(lines[1])
	block := []byte(fields[4])
	for i := range len(block) - 1 {
		if block[i] != block[i+1] {
			block[i], block[i+1] = block[i+1], block[i]
			break
		}
	}
	fields[4] = string(block)
	lines[1] = strings.Join(fields, " ")

	if _, corrections := decodeLines(t, lines); len(corrections) != 2 {
		t.Fatalf("got corrections %v, want 2", corrections)
	}
}

func TestFillsInShortBlock(t *testing.T) {
	lines := formatGrid(t, randomBytes(100))
	fields := strings.Fields(lines[1])
	fields[3] = fields[3][:4]
	lines[1] = strings.Join(fields, " ")

	if _, corrections := decodeLines(t, lines); len(corrections) == 0 {
		t.Fatal("expected the short block to be filled in")
	}
}

func TestSpacingIsIgnored(t *testing.T) {
	lines := formatGrid(t, []byte(sampleText))
	fields := strings.Fields(lines[1])
	lines[1] = fields[0] + " " + strings.Join(fields[1:], "-")
	lines[2] = strings.Join(strings.Fields(lines[2]), "  ")
	if got, _ := decodeLines(t, lines); !bytes.Equal(got, []byte(sampleText)) {
		t.Fatalf("got %q", got)
	}
}

func TestRejectsBadRowAndAcceptsRetype(t *testing.T) {
	lines := formatGrid(t, randomBytes(100))
	d := codec.NewDecoder()
	if _, err := d.ParseLine(lines[0]); err != nil {
		t.Fatal(err)
	}
	bad := replaceChar(t, lines[1], 'A', 2, 'U')
	bad = replaceChar(t, bad, 'C', 2, 'U')
	bad = replaceChar(t, bad, 'E', 2, 'U')
	if _, err := d.ParseLine(bad); err == nil {
		t.Fatal("row with three unknown mistakes accepted")
	}
	// Rows can arrive in any order, including the retyped one.
	for i := len(lines) - 1; i >= 1; i-- {
		if _, err := d.ParseLine(lines[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Plaintext(); err != nil {
		t.Fatal(err)
	}
}

func TestRowBeforeHeaderRejected(t *testing.T) {
	lines := formatGrid(t, []byte(sampleText))
	if _, err := codec.NewDecoder().ParseLine(lines[1]); !errors.Is(err, codec.ErrNoHeader) {
		t.Fatalf("got %v, want ErrNoHeader", err)
	}
}

func TestIncompleteRejected(t *testing.T) {
	lines := formatGrid(t, []byte(sampleText))
	d := codec.NewDecoder()
	for _, line := range lines[:len(lines)-1] {
		if _, err := d.ParseLine(line); err != nil {
			t.Fatal(err)
		}
	}
	if missing := d.Missing(); len(missing) != 1 || missing[0] != len(lines)-1 {
		t.Errorf("missing %v", missing)
	}
	if _, err := d.Plaintext(); !errors.Is(err, codec.ErrIncomplete) {
		t.Fatalf("got %v, want ErrIncomplete", err)
	}
}
