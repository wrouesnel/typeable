package codec

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wrouesnel/typeable/pkg/crockford"
	"github.com/wrouesnel/typeable/pkg/rs32"
)

// maxBlocks bounds the header's block count, so a mistyped header can't allocate
// an absurd number of rows. Nobody is retyping a million blocks.
const maxBlocks = 1 << 20

var (
	// ErrNoHeader is returned when a row is entered before the header line.
	ErrNoHeader = errors.New("the header line (block count) must be entered first")
	// ErrTooManyMistakes is returned when a row has more mistakes than its parity can repair.
	ErrTooManyMistakes = errors.New("too many mistakes to repair, please retype it")
	// ErrIncomplete is returned when plaintext is requested before every row is entered.
	ErrIncomplete = errors.New("not all rows have been entered")
)

// Correction describes one character the decoder repaired.
type Correction struct {
	// Column is the column letter of the block.
	Column byte
	// Offset is the 1-based character position within the block.
	Offset int
	// Typed is the character that was entered, or 0 if it was missing or unreadable.
	Typed rune
	// Correct is the repaired character.
	Correct byte
}

func (c Correction) String() string {
	if c.Typed == 0 {
		return fmt.Sprintf("column %c character %d: filled in %q", c.Column, c.Offset, c.Correct)
	}
	return fmt.Sprintf("column %c character %d: %q corrected to %q", c.Column, c.Offset, c.Typed, c.Correct)
}

// LineResult describes a line accepted by Decoder.ParseLine.
type LineResult struct {
	// Header is true if the line was the header.
	Header bool
	// Row is the 1-based row number, if the line was a row.
	Row int
	// Corrections lists the characters repaired in the row.
	Corrections []Correction
}

// Decoder rebuilds a grid from lines typed by a human. Lines may be entered in any
// order, and a row entered again replaces the earlier one, so a rejected row can simply
// be retyped.
type Decoder struct {
	grid Grid
	have int
}

// NewDecoder returns a Decoder awaiting its header line.
func NewDecoder() *Decoder {
	return &Decoder{}
}

// HaveHeader reports whether the header line has been entered.
func (d *Decoder) HaveHeader() bool {
	return d.grid.Blocks > 0
}

// Missing returns the 1-based numbers of rows not yet entered.
func (d *Decoder) Missing() []int {
	var missing []int
	for r, row := range d.grid.Rows {
		if row == nil {
			missing = append(missing, r+1)
		}
	}
	return missing
}

// Complete reports whether the header and every row have been entered.
func (d *Decoder) Complete() bool {
	return d.HaveHeader() && d.have == len(d.grid.Rows)
}

// Plaintext returns the decoded data once the decoder is complete.
func (d *Decoder) Plaintext() ([]byte, error) {
	if !d.Complete() {
		return nil, ErrIncomplete
	}
	return d.grid.Plaintext()
}

// ParseLine accepts one line of input. Blank lines and lines starting with # are
// ignored, returning nil. Whitespace and hyphens between characters are not significant.
func (d *Decoder) ParseLine(line string) (*LineResult, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
		return nil, nil
	}
	number, err := strconv.Atoi(strings.TrimRight(fields[0], ".:"))
	if err != nil || number < 1 {
		return nil, fmt.Errorf("line must start with a row number, got %q", fields[0])
	}
	if !d.HaveHeader() {
		return d.parseHeader(number, fields[1:])
	}
	return d.SetRow(number, fields[1:])
}

func (d *Decoder) parseHeader(blocks int, labels []string) (*LineResult, error) {
	for _, label := range labels {
		if len(label) != 1 || !strings.Contains(Columns, strings.ToUpper(label)) {
			return nil, fmt.Errorf("%w: expected a block count followed by column letters, got %q",
				ErrNoHeader, label)
		}
	}
	if err := d.SetBlocks(blocks); err != nil {
		return nil, err
	}
	return &LineResult{Header: true}, nil
}

// SetBlocks sets the number of data blocks from the header, discarding any rows.
func (d *Decoder) SetBlocks(blocks int) error {
	if blocks < 1 || blocks > maxBlocks {
		return fmt.Errorf("%w: block count %d is out of range", ErrNoHeader, blocks)
	}
	d.grid = Grid{Blocks: blocks, Rows: make([][]byte, rowCount(blocks))}
	d.have = 0
	return nil
}

// Blocks returns the number of data blocks, or 0 before the header.
func (d *Decoder) Blocks() int {
	return d.grid.Blocks
}

// Rows returns the number of rows, or 0 before the header.
func (d *Decoder) Rows() int {
	return len(d.grid.Rows)
}

// RowBlocks returns the number of data blocks in a 1-based row. Only the last row can
// have fewer than DataBlocksPerRow.
func (d *Decoder) RowBlocks(number int) int {
	return rowDataBlocks(d.grid.Blocks, number-1)
}

// ClearRow forgets a 1-based row, for example because it has since been edited.
func (d *Decoder) ClearRow(number int) {
	if number >= 1 && number <= len(d.grid.Rows) && d.grid.Rows[number-1] != nil {
		d.grid.Rows[number-1] = nil
		d.have--
	}
}

// SetRow checks and repairs the blocks typed for a 1-based row, and stores it if it is
// valid. fields are its data blocks followed by the parity block. On error any earlier
// entry of the row is kept.
func (d *Decoder) SetRow(number int, fields []string) (*LineResult, error) {
	if !d.HaveHeader() {
		return nil, ErrNoHeader
	}
	if number < 1 || number > len(d.grid.Rows) {
		return nil, fmt.Errorf("row %d: there are only %d rows", number, len(d.grid.Rows))
	}
	nBlocks := rowDataBlocks(d.grid.Blocks, number-1)
	row, typed, erasures, err := readRow(fields, nBlocks)
	if err != nil {
		return nil, fmt.Errorf("row %d: %w", number, err)
	}

	changed, err := decodeRow(row, erasures)
	if err != nil {
		if errors.Is(err, rs32.ErrUncorrectable) {
			return nil, fmt.Errorf("row %d: %w", number, ErrTooManyMistakes)
		}
		return nil, fmt.Errorf("row %d: %w", number, err)
	}

	// Unknown characters are always repaired, even if the placeholder happened to be right.
	for _, pos := range erasures {
		if !slices.Contains(changed, pos) {
			changed = append(changed, pos)
		}
	}
	slices.Sort(changed)

	result := &LineResult{Row: number}
	for _, pos := range changed {
		column := Columns[pos/BlockSize]
		if pos >= nBlocks*BlockSize {
			column = Columns[DataBlocksPerRow]
		}
		result.Corrections = append(result.Corrections, Correction{
			Column:  column,
			Offset:  pos%BlockSize + 1,
			Typed:   typed[pos],
			Correct: lowerChar(row[pos]),
		})
	}

	if d.grid.Rows[number-1] == nil {
		d.have++
	}
	d.grid.Rows[number-1] = row
	return result, nil
}

// readRow converts the blocks typed for a row with nBlocks data blocks into symbols. It
// returns the character typed at each position (0 if none) and the positions that are
// unknown. If the characters add up to the right length the spacing is ignored;
// otherwise each block must be separate, and blocks of the wrong length are treated as
// unknown so the parity can fill them in.
func readRow(fields []string, nBlocks int) ([]byte, []rune, []int, error) {
	size := (nBlocks + 1) * BlockSize
	row := make([]byte, size)
	typed := make([]rune, size)
	var erasures []int

	blocks := make([]string, len(fields))
	total := 0
	for i, field := range fields {
		blocks[i] = strings.ReplaceAll(field, "-", "")
		total += utf8.RuneCountInString(blocks[i])
	}

	var cells []string
	switch {
	case total == size:
		cells = []string{strings.Join(blocks, "")}
	case len(blocks) == nBlocks+1:
		cells = blocks
	default:
		return nil, nil, nil, fmt.Errorf("expected %d blocks of %d characters, got %d characters in %d blocks",
			nBlocks+1, BlockSize, total, len(blocks))
	}

	pos := 0
	for _, cell := range cells {
		if len(cells) > 1 && utf8.RuneCountInString(cell) != BlockSize {
			for range BlockSize {
				erasures = append(erasures, pos)
				pos++
			}
			continue
		}
		for _, r := range cell {
			typed[pos] = r
			if symbol, ok := crockford.Symbol(r); ok {
				row[pos] = symbol
			} else {
				erasures = append(erasures, pos)
			}
			pos++
		}
	}
	return row, typed, erasures, nil
}
