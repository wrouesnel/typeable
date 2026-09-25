// Package decodetui is an interactive terminal editor for typing in a typeable grid.
// It asks for the number of data blocks, then shows an empty grid of blocks to fill in.
// Rows are checked as they are typed, repaired characters are highlighted, and rows
// with too many mistakes are flagged so they can be fixed before decoding.
package decodetui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wrouesnel/typeable/pkg/codec"
	"github.com/wrouesnel/typeable/pkg/crockford"
)

// parityColumn is the column index of the parity block, which is the same in every row.
const parityColumn = codec.DataBlocksPerRow

// Lines above and below the rows in the grid view.
const (
	headerLines = 3
	footerLines = 4
)

type phase int

const (
	phaseCount phase = iota
	phaseGrid
)

type rowState int

const (
	rowEmpty rowState = iota
	rowEditing
	rowValid
	rowInvalid
)

type rowStatus struct {
	state       rowState
	corrections []codec.Correction
	err         error
}

//nolint:gochecknoglobals // immutable styles
var (
	styleTitle     = lipgloss.NewStyle().Bold(true)
	styleLabel     = lipgloss.NewStyle().Faint(true)
	styleEmpty     = lipgloss.NewStyle().Faint(true)
	styleNone      = lipgloss.NewStyle()
	styleCorrected = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	styleValid     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleInvalid   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleHelp      = lipgloss.NewStyle().Faint(true)
)

// Model is the Bubble Tea model for the editor.
type Model struct {
	decoder *codec.Decoder
	phase   phase

	countInput string
	message    string

	// cells holds the text typed into each block: cells[row][i] for data blocks, with
	// the parity block last.
	cells  [][]string
	status []rowStatus

	// row and cell locate the focused block, and pos the cursor within it.
	row, cell, pos int

	offset int
	height int

	plaintext []byte
}

// New returns an editor asking for the block count.
func New() *Model {
	return &Model{decoder: codec.NewDecoder()}
}

// Plaintext returns the decoded data, or nil if the editor was quit before decoding.
func (m *Model) Plaintext() []byte {
	return m.plaintext
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.scroll()
	case tea.PasteMsg:
		if m.phase == phaseGrid {
			m.paste(msg.Content)
		}
	case tea.KeyPressMsg:
		if key := msg.String(); key == "ctrl+c" || key == "esc" {
			return m, tea.Quit
		}
		m.message = ""
		if m.phase == phaseCount {
			return m, m.updateCount(msg)
		}
		return m, m.updateGrid(msg)
	}
	return m, nil
}

func (m *Model) updateCount(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "backspace":
		if m.countInput != "" {
			m.countInput = m.countInput[:len(m.countInput)-1]
		}
	case "enter":
		blocks, err := strconv.Atoi(m.countInput)
		if err != nil {
			m.message = "Enter the number of data blocks shown at the top left of the grid."
			return nil
		}
		if err := m.decoder.SetBlocks(blocks); err != nil {
			m.message = err.Error()
			return nil
		}
		m.startGrid()
	default:
		if len(msg.Text) == 1 && msg.Text[0] >= '0' && msg.Text[0] <= '9' && len(m.countInput) < 7 {
			m.countInput += msg.Text
		}
	}
	return nil
}

func (m *Model) startGrid() {
	m.phase = phaseGrid
	m.cells = make([][]string, m.decoder.Rows())
	m.status = make([]rowStatus, m.decoder.Rows())
	for r := range m.cells {
		m.cells[r] = make([]string, m.decoder.RowBlocks(r+1)+1)
	}
	m.row, m.cell, m.pos, m.offset = 0, 0, 0, 0
}

func (m *Model) updateGrid(msg tea.KeyPressMsg) tea.Cmd {
	prevRow := m.row
	switch msg.String() {
	case "up":
		m.moveRow(m.row - 1)
	case "down":
		m.moveRow(m.row + 1)
	case "left":
		if m.pos > 0 {
			m.pos--
		} else if m.prevCell() {
			m.pos = max(0, len(m.cells[m.row][m.cell])-1)
		}
	case "right":
		if m.pos < len(m.cells[m.row][m.cell]) && m.pos < codec.BlockSize-1 {
			m.pos++
		} else {
			m.nextCell()
		}
	case "tab", "space":
		m.nextCell()
	case "shift+tab":
		m.prevCell()
	case "home":
		m.cell, m.pos = 0, 0
	case "end":
		m.cell = len(m.cells[m.row]) - 1
		m.pos = min(len(m.cells[m.row][m.cell]), codec.BlockSize-1)
	case "backspace":
		m.backspace()
	case "delete":
		text := m.cells[m.row][m.cell]
		if m.pos < len(text) {
			m.cells[m.row][m.cell] = text[:m.pos] + text[m.pos+1:]
		}
	case "enter", "ctrl+d":
		if m.decoder.Complete() {
			return m.finish()
		}
		m.nextUnfinishedRow()
	default:
		if len(msg.Text) == 1 {
			m.typeChar(rune(msg.Text[0]))
		}
	}
	m.check(prevRow)
	if m.row != prevRow {
		m.check(m.row)
	}
	m.scroll()
	return nil
}

// typeChar enters a character at the cursor, overwriting what's there, and moves on to
// the next block once this one is full.
func (m *Model) typeChar(r rune) {
	if _, ok := crockford.Symbol(r); !ok {
		m.message = fmt.Sprintf("%q is not used in typeable grids.", r)
		return
	}
	c := strings.ToLower(string(r))
	text := m.cells[m.row][m.cell]
	if m.pos < len(text) {
		text = text[:m.pos] + c + text[m.pos+1:]
	} else {
		text += c
	}
	m.cells[m.row][m.cell] = text
	if m.pos < codec.BlockSize-1 {
		m.pos++
	} else {
		m.nextCell()
	}
}

func (m *Model) backspace() {
	text := m.cells[m.row][m.cell]
	if m.pos > 0 {
		m.cells[m.row][m.cell] = text[:m.pos-1] + text[m.pos:]
		m.pos--
		return
	}
	if !m.prevCell() {
		return
	}
	text = m.cells[m.row][m.cell]
	if text != "" {
		text = text[:len(text)-1]
		m.cells[m.row][m.cell] = text
	}
	m.pos = len(text)
}

// paste types pasted text: whitespace moves to the next block and newlines to the next
// row, so a row copied from an encoded grid (without its row number) can be pasted in.
func (m *Model) paste(content string) {
	for _, r := range content {
		prevRow := m.row
		switch r {
		case '\n':
			m.moveRow(m.row + 1)
			m.cell, m.pos = 0, 0
		case ' ', '\t', '\r', '-':
			if m.pos > 0 {
				m.nextCell()
			}
		default:
			m.typeChar(r)
		}
		if m.row != prevRow {
			m.check(prevRow)
		}
	}
	m.check(m.row)
	m.scroll()
}

// nextCell moves to the start of the next block, wrapping onto the next row.
func (m *Model) nextCell() {
	if m.cell < len(m.cells[m.row])-1 {
		m.cell++
	} else if m.row < len(m.cells)-1 {
		m.row++
		m.cell = 0
	} else {
		m.pos = min(m.pos, len(m.cells[m.row][m.cell]))
		return
	}
	m.pos = 0
}

// prevCell moves to the end of the previous block, wrapping onto the previous row. It
// reports false at the start of the grid.
func (m *Model) prevCell() bool {
	switch {
	case m.cell > 0:
		m.cell--
	case m.row > 0:
		m.row--
		m.cell = len(m.cells[m.row]) - 1
	default:
		return false
	}
	m.pos = min(len(m.cells[m.row][m.cell]), codec.BlockSize-1)
	return true
}

// column returns the column index of a block in a row.
func (m *Model) column(row, cell int) int {
	if cell == len(m.cells[row])-1 {
		return parityColumn
	}
	return cell
}

// moveRow moves to another row, staying in the same column where possible.
func (m *Model) moveRow(row int) {
	if row < 0 || row >= len(m.cells) {
		return
	}
	column := m.column(m.row, m.cell)
	m.row = row
	last := len(m.cells[row]) - 1
	if column == parityColumn {
		m.cell = last
	} else {
		m.cell = min(column, last-1)
	}
	m.pos = min(m.pos, len(m.cells[row][m.cell]))
}

func (m *Model) nextUnfinishedRow() {
	for step := 1; step <= len(m.cells); step++ {
		row := (m.row + step) % len(m.cells)
		if m.status[row].state != rowValid {
			m.row, m.cell, m.pos = row, 0, 0
			return
		}
	}
}

// check validates a row. A full row is checked straight away. A partly typed row is
// only checked once the cursor leaves it, so its blanks count as unknown characters
// for the parity to fill in rather than as mistakes still being typed.
func (m *Model) check(row int) {
	cells := m.cells[row]
	full, empty := true, true
	for _, text := range cells {
		if len(text) != codec.BlockSize {
			full = false
		}
		if text != "" {
			empty = false
		}
	}

	status := &m.status[row]
	*status = rowStatus{}
	switch {
	case empty:
		status.state = rowEmpty
	case !full && row == m.row:
		status.state = rowEditing
	default:
		result, err := m.decoder.SetRow(row+1, cells)
		if err != nil {
			status.state = rowInvalid
			status.err = err
		} else {
			status.state = rowValid
			status.corrections = result.Corrections
			return
		}
	}
	m.decoder.ClearRow(row + 1)
}

func (m *Model) finish() tea.Cmd {
	plaintext, err := m.decoder.Plaintext()
	if err != nil {
		m.message = err.Error()
		return nil
	}
	m.plaintext = plaintext
	return tea.Quit
}

func (m *Model) visibleRows() int {
	if m.height == 0 {
		return len(m.cells)
	}
	return max(1, m.height-headerLines-footerLines)
}

// scroll keeps the focused row on screen.
func (m *Model) scroll() {
	visible := m.visibleRows()
	if m.row < m.offset {
		m.offset = m.row
	} else if m.row >= m.offset+visible {
		m.offset = m.row - visible + 1
	}
}
