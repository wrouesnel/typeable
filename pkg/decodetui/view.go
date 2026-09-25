package decodetui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/wrouesnel/typeable/pkg/codec"
)

const countPrompt = "Number of data blocks (the number at the top left of the grid): "

// View implements tea.View.
func (m *Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	if m.phase == phaseCount {
		lines := []string{
			styleTitle.Render("typeable decode"),
			"",
			countPrompt + m.countInput,
			"",
			m.message,
			styleHelp.Render("enter continue · esc quit"),
		}
		view.SetContent(strings.Join(lines, "\n"))
		view.Cursor = tea.NewCursor(len(countPrompt)+len(m.countInput), 2)
		return view
	}

	labelWidth := max(len(strconv.Itoa(m.decoder.Blocks())), len(strconv.Itoa(len(m.cells))))
	valid := 0
	for _, status := range m.status {
		if status.state == rowValid {
			valid++
		}
	}

	lines := make([]string, 0, headerLines+m.visibleRows()+footerLines)
	lines = append(lines,
		styleTitle.Render(fmt.Sprintf("typeable decode: %d blocks in %d rows, %d rows valid",
			m.decoder.Blocks(), len(m.cells), valid)),
		"",
		m.headerLine(labelWidth))
	end := min(len(m.cells), m.offset+m.visibleRows())
	for row := m.offset; row < end; row++ {
		lines = append(lines, m.rowLine(row, labelWidth))
	}
	lines = append(lines, "", m.detailLine(), m.message)
	if m.decoder.Complete() {
		lines = append(lines, styleValid.Render("All rows valid. Press enter to decode.")+
			styleHelp.Render(" · esc quit"))
	} else {
		lines = append(lines, styleHelp.Render(
			"arrows move · tab/space next block · backspace delete · enter next unfinished row · esc quit"))
	}
	view.SetContent(strings.Join(lines, "\n"))
	view.Cursor = tea.NewCursor(cellX(labelWidth, m.column(m.row, m.cell))+m.pos, headerLines+m.row-m.offset)
	return view
}

// cellX returns the screen column where a block starts. The layout matches
// codec.Grid.Format: the label, 3 spaces, data blocks separated by spaces, then 3
// spaces before the parity block.
func cellX(labelWidth, column int) int {
	x := labelWidth + 3 + column*(codec.BlockSize+1)
	if column == parityColumn {
		x += 2
	}
	return x
}

func (m *Model) headerLine(labelWidth int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-*d", labelWidth, m.decoder.Blocks())
	for column, letter := range codec.Columns {
		sb.WriteString(strings.Repeat(" ", cellX(labelWidth, column)+codec.BlockSize/2-sb.Len()))
		sb.WriteRune(letter)
	}
	return styleLabel.Render(sb.String())
}

func (m *Model) rowLine(row, labelWidth int) string {
	status := m.status[row]
	// Repaired characters by column and offset, so the grid shows what will be decoded.
	corrected := map[[2]int]byte{}
	for _, c := range status.corrections {
		column := strings.IndexByte(codec.Columns, c.Column)
		corrected[[2]int{column, c.Offset - 1}] = c.Correct
	}

	var sb strings.Builder
	sb.WriteString(styleLabel.Render(fmt.Sprintf("%-*d", labelWidth, row+1)))
	x := labelWidth
	for cell, text := range m.cells[row] {
		column := m.column(row, cell)
		start := cellX(labelWidth, column)
		sb.WriteString(strings.Repeat(" ", start-x))
		for i := range codec.BlockSize {
			ch, style := "·", styleEmpty
			if i < len(text) {
				ch, style = text[i:i+1], styleNone
			}
			if fix, ok := corrected[[2]int{column, i}]; ok {
				ch, style = string(fix), styleCorrected
			}
			if row == m.row && cell == m.cell {
				style = style.Underline(true)
			}
			sb.WriteString(style.Render(ch))
		}
		x = start + codec.BlockSize
	}
	sb.WriteString(strings.Repeat(" ", cellX(labelWidth, parityColumn)+codec.BlockSize-x+2))

	switch status.state {
	case rowValid:
		if len(status.corrections) > 0 {
			sb.WriteString(styleCorrected.Render(fmt.Sprintf("✓ %d fixed", len(status.corrections))))
		} else {
			sb.WriteString(styleValid.Render("✓"))
		}
	case rowInvalid:
		sb.WriteString(styleInvalid.Render("✗ retype"))
	case rowEmpty, rowEditing:
	}
	return sb.String()
}

// detailLine explains the focused row's status.
func (m *Model) detailLine() string {
	status := m.status[m.row]
	switch status.state {
	case rowInvalid:
		return styleInvalid.Render(status.err.Error())
	case rowValid:
		if len(status.corrections) == 0 {
			return ""
		}
		fixes := make([]string, 0, len(status.corrections))
		for _, c := range status.corrections {
			fixes = append(fixes, c.String())
		}
		return styleCorrected.Render(fmt.Sprintf("row %d: %s", m.row+1, strings.Join(fixes, "; ")))
	case rowEmpty, rowEditing:
	}
	return ""
}
