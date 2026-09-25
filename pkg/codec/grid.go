// Package codec implements the typeable grid format: data compressed, written in
// Crockford base32, and laid out in rows of 5-character blocks with a parity block
// per row so that mistyped rows can be detected and repaired.
package codec

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/wrouesnel/typeable/pkg/crockford"
	"github.com/wrouesnel/typeable/pkg/rs32"
)

const (
	// BlockSize is the number of characters in a block.
	BlockSize = 5
	// DataBlocksPerRow is the number of data blocks in a full row.
	DataBlocksPerRow = 7
	// Columns are the column labels: the data columns followed by the parity column.
	Columns = "ABCDEFGH"

	// lanes is how many interleaved codewords a row is split into. A GF(32) codeword
	// holds at most 31 characters, too few for a full row. Lane 0 holds the 2nd and 4th
	// character of every block and lane 1 the 1st, 3rd and 5th, so the parity block
	// gives lane 0 two parity characters and lane 1 three. That way a whole block can
	// be unknown (two or three characters per lane) and still be filled in, and two
	// swapped characters within a block are a single error in each lane.
	lanes = 2
)

// Grid is an encoded body of data.
type Grid struct {
	// Blocks is the total number of data blocks.
	Blocks int
	// Rows holds the symbols of each row: its data blocks followed by a parity block.
	Rows [][]byte
}

// rowCount returns how many rows hold the given number of data blocks.
func rowCount(blocks int) int {
	return (blocks + DataBlocksPerRow - 1) / DataBlocksPerRow
}

// rowDataBlocks returns the number of data blocks in row (0-indexed). Only the last row
// can be short.
func rowDataBlocks(blocks, row int) int {
	return min(DataBlocksPerRow, blocks-row*DataBlocksPerRow)
}

// laneOf returns the lane of a character position within a row.
func laneOf(pos int) int {
	return 1 - (pos%BlockSize)%2
}

// lane returns the positions within a row of nData data symbols (plus parity) that form
// one codeword, and how many of them are parity.
func lane(nData, index int) ([]int, int) {
	var positions []int
	nParity := 0
	for pos := range nData + BlockSize {
		if laneOf(pos) != index {
			continue
		}
		positions = append(positions, pos)
		if pos >= nData {
			nParity++
		}
	}
	return positions, nParity
}

// encodeRow returns data followed by its parity block.
func encodeRow(data []byte) ([]byte, error) {
	row := make([]byte, len(data)+BlockSize)
	copy(row, data)
	for index := range lanes {
		positions, nParity := lane(len(data), index)
		laneData := make([]byte, 0, len(positions)-nParity)
		for _, pos := range positions[:len(positions)-nParity] {
			laneData = append(laneData, row[pos])
		}
		parity, err := rs32.Encode(laneData, nParity)
		if err != nil {
			return nil, err
		}
		for i, pos := range positions[len(positions)-nParity:] {
			row[pos] = parity[i]
		}
	}
	return row, nil
}

// decodeRow corrects row (data followed by parity) in place, treating the positions in
// erasures as unknown. It returns the positions it changed.
func decodeRow(row []byte, erasures []int) ([]int, error) {
	erased := make(map[int]bool, len(erasures))
	for _, pos := range erasures {
		erased[pos] = true
	}
	var changed []int
	for index := range lanes {
		positions, nParity := lane(len(row)-BlockSize, index)
		codeword := make([]byte, len(positions))
		var laneErasures []int
		for i, pos := range positions {
			codeword[i] = row[pos]
			if erased[pos] {
				laneErasures = append(laneErasures, i)
			}
		}
		fixed, err := rs32.Decode(codeword, nParity, laneErasures)
		if err != nil {
			return nil, err
		}
		for _, i := range fixed {
			row[positions[i]] = codeword[i]
			changed = append(changed, positions[i])
		}
	}
	return changed, nil
}

// Encode compresses plaintext and lays it out as a grid.
func Encode(plaintext []byte) (*Grid, error) {
	payload, err := compress(plaintext)
	if err != nil {
		return nil, err
	}
	symbols := crockford.FromBytes(payload)
	for len(symbols)%BlockSize != 0 {
		symbols = append(symbols, 0)
	}

	grid := &Grid{Blocks: len(symbols) / BlockSize}
	for r := range rowCount(grid.Blocks) {
		start := r * DataBlocksPerRow * BlockSize
		end := start + rowDataBlocks(grid.Blocks, r)*BlockSize
		row, err := encodeRow(symbols[start:end])
		if err != nil {
			return nil, err
		}
		grid.Rows = append(grid.Rows, row)
	}
	return grid, nil
}

// Plaintext decodes the grid's data back to the original plaintext.
func (g *Grid) Plaintext() ([]byte, error) {
	symbols := make([]byte, 0, g.Blocks*BlockSize)
	for _, row := range g.Rows {
		symbols = append(symbols, row[:len(row)-BlockSize]...)
	}
	return decompress(crockford.ToBytes(symbols))
}

// Format writes the grid as text for a human to retype. The top-left cell is the number
// of data blocks, columns are lettered and rows are numbered from 1.
func (g *Grid) Format(w io.Writer) error {
	labelWidth := max(len(strconv.Itoa(g.Blocks)), len(strconv.Itoa(len(g.Rows))))
	blank := strings.Repeat(" ", BlockSize)

	cells := make([]string, 0, len(Columns))
	for _, col := range Columns {
		cells = append(cells, fmt.Sprintf("%*c%*s", BlockSize/2+1, col, BlockSize-BlockSize/2-1, ""))
	}
	if err := writeLine(w, labelWidth, strconv.Itoa(g.Blocks), cells); err != nil {
		return err
	}

	for r, row := range g.Rows {
		cells = cells[:0]
		nData := len(row) - BlockSize
		for start := 0; start < nData; start += BlockSize {
			cells = append(cells, symbolsToString(row[start:start+BlockSize]))
		}
		// Short rows keep the parity block under column H.
		for len(cells) < DataBlocksPerRow {
			cells = append(cells, blank)
		}
		cells = append(cells, symbolsToString(row[nData:]))
		if err := writeLine(w, labelWidth, strconv.Itoa(r+1), cells); err != nil {
			return err
		}
	}
	return nil
}

func writeLine(w io.Writer, labelWidth int, label string, cells []string) error {
	parity := len(cells) - 1
	line := fmt.Sprintf("%-*s   %s   %s", labelWidth, label,
		strings.Join(cells[:parity], " "), cells[parity])
	_, err := fmt.Fprintln(w, strings.TrimRight(line, " "))
	return err
}

// lowerChar returns the lowercase character for a symbol. Blocks are written in
// lowercase, which is easier to read and type; decoding accepts either case.
func lowerChar(symbol byte) byte {
	c := crockford.Char(symbol)
	if c >= 'A' && c <= 'Z' {
		c += 'a' - 'A'
	}
	return c
}

func symbolsToString(symbols []byte) string {
	var sb strings.Builder
	for _, s := range symbols {
		sb.WriteByte(lowerChar(s))
	}
	return sb.String()
}
