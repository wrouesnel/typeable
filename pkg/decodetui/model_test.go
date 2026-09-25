package decodetui_test

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wrouesnel/typeable/pkg/codec"
	"github.com/wrouesnel/typeable/pkg/decodetui"
)

const plaintext = "Some text which is long enough to need a few rows of blocks to type in.\n"

// encodedRows returns the block count and the blocks of each row of an encoded grid.
func encodedRows(t *testing.T) (string, [][]string) {
	t.Helper()
	grid, err := codec.Encode([]byte(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := grid.Format(&buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	rows := make([][]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		rows = append(rows, strings.Fields(line)[1:])
	}
	return strings.Fields(lines[0])[0], rows
}

var specialKeys = map[string]tea.KeyPressMsg{
	"up":        {Code: tea.KeyUp},
	"down":      {Code: tea.KeyDown},
	"left":      {Code: tea.KeyLeft},
	"right":     {Code: tea.KeyRight},
	"home":      {Code: tea.KeyHome},
	"tab":       {Code: tea.KeyTab},
	"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
	"enter":     {Code: tea.KeyEnter},
	"backspace": {Code: tea.KeyBackspace},
	"esc":       {Code: tea.KeyEscape},
	"space":     {Code: tea.KeySpace, Text: " "},
}

// press sends keys to the model: names from specialKeys, or text typed a rune at a time.
func press(t *testing.T, m *decodetui.Model, keys ...string) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, key := range keys {
		if msg, ok := specialKeys[key]; ok {
			_, cmd = m.Update(msg)
			if msg.String() != key {
				t.Fatalf("test bug: key %q reports as %q", key, msg.String())
			}
			continue
		}
		for _, r := range key {
			_, cmd = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	return cmd
}

// isQuit reports whether cmd quits the program.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func startEditor(t *testing.T) (*decodetui.Model, [][]string) {
	t.Helper()
	count, rows := encodedRows(t)
	m := decodetui.New()
	press(t, m, count, "enter")
	return m, rows
}

func TestTypeWholeGrid(t *testing.T) {
	m, rows := startEditor(t)
	// Full blocks advance to the next block and row on their own.
	for _, row := range rows {
		press(t, m, strings.Join(row, ""))
	}
	if !strings.Contains(m.View().Content, "All rows valid") {
		t.Fatalf("grid not valid:\n%s", m.View().Content)
	}
	if !isQuit(press(t, m, "enter")) {
		t.Fatal("enter did not finish")
	}
	if got := string(m.Plaintext()); got != plaintext {
		t.Fatalf("got %q, want %q", got, plaintext)
	}
}

func TestRepairIsShown(t *testing.T) {
	m, rows := startEditor(t)
	for r, row := range rows {
		text := strings.Join(row, "")
		if r == 0 {
			// Wrong first character, uppercase entry of the rest.
			wrong := "0"
			if text[0] == '0' {
				wrong = "1"
			}
			text = wrong + strings.ToUpper(text[1:])
		}
		press(t, m, text)
	}
	view := m.View().Content
	if !strings.Contains(view, "1 fixed") || !strings.Contains(view, "All rows valid") {
		t.Fatalf("repair not shown:\n%s", view)
	}
}

func TestArrowKeysFixRejectedRow(t *testing.T) {
	m, rows := startEditor(t)
	// Three mistakes in the second character of blocks A, C and E of row 1 are more
	// than the parity can repair.
	first := strings.Join(rows[0], "")
	bad := []byte(first)
	for _, pos := range []int{1, 11, 21} {
		bad[pos] = map[bool]byte{true: 'z', false: 'y'}[bad[pos] != 'z']
	}
	press(t, m, string(bad))
	for _, row := range rows[1:] {
		press(t, m, strings.Join(row, ""))
	}
	if view := m.View().Content; !strings.Contains(view, "retype") || strings.Contains(view, "All rows valid") {
		t.Fatalf("bad row not rejected:\n%s", view)
	}

	// Back up to row 1 and fix each mistake by moving with the arrow keys.
	for range len(rows) {
		press(t, m, "up")
	}
	press(t, m, "home", "right", first[1:2])
	press(t, m, "tab", "tab", "right", first[11:12])
	press(t, m, "tab", "tab", "right", first[21:22])
	press(t, m, "down")

	if view := m.View().Content; !strings.Contains(view, "All rows valid") {
		t.Fatalf("grid not valid after fixing:\n%s", view)
	}
	if !isQuit(press(t, m, "enter")) || string(m.Plaintext()) != plaintext {
		t.Fatalf("got %q", m.Plaintext())
	}
}

func TestBlankCharactersFilledIn(t *testing.T) {
	m, rows := startEditor(t)
	for r, row := range rows {
		if r == 0 {
			// Leave block B empty, then move on to the next row.
			press(t, m, row[0], "tab", strings.Join(row[2:], ""))
			continue
		}
		press(t, m, strings.Join(row, ""))
	}
	view := m.View().Content
	if !strings.Contains(view, "5 fixed") || !strings.Contains(view, "All rows valid") {
		t.Fatalf("blank block not filled in:\n%s", view)
	}
}

func TestBackspaceAcrossBlocks(t *testing.T) {
	m, rows := startEditor(t)
	press(t, m, rows[0][0], "x", "backspace", "backspace", rows[0][0][4:])
	press(t, m, strings.Join(rows[0][1:], ""))
	for _, row := range rows[1:] {
		press(t, m, strings.Join(row, ""))
	}
	if view := m.View().Content; !strings.Contains(view, "All rows valid") || strings.Contains(view, "fixed") {
		t.Fatalf("unexpected state:\n%s", view)
	}
}

func TestInvalidCharacterRejected(t *testing.T) {
	m, _ := startEditor(t)
	press(t, m, "u")
	if view := m.View().Content; !strings.Contains(view, `'u' is not used`) {
		t.Fatalf("invalid character not reported:\n%s", view)
	}
}

func TestBadCountRejected(t *testing.T) {
	m := decodetui.New()
	press(t, m, "0", "enter")
	if view := m.View().Content; !strings.Contains(view, "out of range") {
		t.Fatalf("zero block count accepted:\n%s", view)
	}
}

func TestEscapeCancels(t *testing.T) {
	m, _ := startEditor(t)
	if !isQuit(press(t, m, "esc")) || m.Plaintext() != nil {
		t.Fatal("escape did not cancel")
	}
}
