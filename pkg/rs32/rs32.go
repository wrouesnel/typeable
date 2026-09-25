// Package rs32 implements Reed-Solomon error correction over GF(32), so that each
// symbol is exactly one base32 character.
//
// Codewords are at most MaxLength symbols long. Decoding searches directly for the
// smallest error pattern that explains the syndromes, which is simple and exact but
// only practical for the small parity counts (a handful of symbols) typeable uses.
package rs32

import (
	"errors"
	"fmt"
)

// MaxLength is the longest codeword (data plus parity) GF(32) supports.
const MaxLength = 31

const (
	fieldSize = 32
	// primitivePoly is x^5 + x^2 + 1.
	primitivePoly = 0x25
)

var (
	// ErrUncorrectable is returned when there are more errors than the parity can repair.
	ErrUncorrectable = errors.New("too many errors to correct")
	// ErrInvalidSymbol is returned when a symbol is outside GF(32).
	ErrInvalidSymbol = errors.New("symbol out of range")
	// ErrInvalidLength is returned when a codeword is too long, or parity is out of range.
	ErrInvalidLength = errors.New("invalid codeword length")
)

// field holds the exponent and logarithm tables for GF(32). exp is doubled so products
// can index it without a modulo.
type field struct {
	exp [2 * (fieldSize - 1)]byte
	log [fieldSize]byte
}

//nolint:gochecknoglobals // immutable lookup tables
var gf = newField()

func newField() *field {
	f := &field{}
	x := 1
	for i := range fieldSize - 1 {
		f.exp[i] = byte(x)
		f.exp[i+fieldSize-1] = byte(x)
		f.log[x] = byte(i)
		x <<= 1
		if x&fieldSize != 0 {
			x ^= primitivePoly
		}
	}
	return f
}

func (f *field) mul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return f.exp[int(f.log[a])+int(f.log[b])]
}

func (f *field) div(a, b byte) byte {
	if a == 0 {
		return 0
	}
	return f.exp[int(f.log[a])+fieldSize-1-int(f.log[b])]
}

// alphaPow returns alpha^n.
func (f *field) alphaPow(n int) byte {
	return f.exp[n%(fieldSize-1)]
}

// generator returns the generator polynomial (x - a^0)(x - a^1)...(x - a^(nParity-1)),
// highest degree coefficient first.
func generator(nParity int) []byte {
	g := []byte{1}
	for i := range nParity {
		root := gf.alphaPow(i)
		next := make([]byte, len(g)+1)
		for j, coeff := range g {
			next[j] ^= coeff
			next[j+1] ^= gf.mul(coeff, root)
		}
		g = next
	}
	return g
}

func checkSymbols(symbols []byte) error {
	for i, s := range symbols {
		if s >= fieldSize {
			return fmt.Errorf("%w: %d at position %d", ErrInvalidSymbol, s, i)
		}
	}
	return nil
}

func checkLength(n, nParity int) error {
	if nParity < 1 || n > MaxLength || n <= nParity {
		return fmt.Errorf("%w: %d symbols with %d parity", ErrInvalidLength, n, nParity)
	}
	return nil
}

// Encode returns nParity parity symbols for data. The codeword is data followed by the
// parity symbols.
func Encode(data []byte, nParity int) ([]byte, error) {
	if err := checkLength(len(data)+nParity, nParity); err != nil {
		return nil, err
	}
	if err := checkSymbols(data); err != nil {
		return nil, err
	}
	g := generator(nParity)
	parity := make([]byte, nParity)
	for _, d := range data {
		feedback := d ^ parity[0]
		copy(parity, parity[1:])
		parity[nParity-1] = 0
		for j := range nParity {
			parity[j] ^= gf.mul(feedback, g[j+1])
		}
	}
	return parity, nil
}

// syndromes evaluates the codeword at each root of the generator.
func syndromes(codeword []byte, nParity int) ([]byte, bool) {
	s := make([]byte, nParity)
	clean := true
	for j := range nParity {
		root := gf.alphaPow(j)
		var acc byte
		for _, c := range codeword {
			acc = gf.mul(acc, root) ^ c
		}
		s[j] = acc
		if acc != 0 {
			clean = false
		}
	}
	return s, clean
}

// Decode corrects codeword in place. Its last nParity symbols are parity. erasures lists
// positions known to be wrong (for example unreadable characters); their current value
// is ignored. Each unknown error costs two parity symbols and each erasure costs one.
// Decode returns the positions whose value it changed.
func Decode(codeword []byte, nParity int, erasures []int) ([]int, error) {
	n := len(codeword)
	if err := checkLength(n, nParity); err != nil {
		return nil, err
	}
	if err := checkSymbols(codeword); err != nil {
		return nil, err
	}
	erased := make(map[int]bool, len(erasures))
	for _, pos := range erasures {
		if pos < 0 || pos >= n {
			return nil, fmt.Errorf("%w: erasure position %d", ErrInvalidLength, pos)
		}
		erased[pos] = true
	}
	if len(erased) > nParity {
		return nil, ErrUncorrectable
	}
	known := make([]int, 0, len(erased))
	for pos := range n {
		if erased[pos] {
			known = append(known, pos)
		}
	}

	synd, clean := syndromes(codeword, nParity)
	if clean {
		return nil, nil
	}

	// Try the fewest unknown errors first: any solution within the correction capacity
	// is unique, so the first one found is the answer.
	candidates := make([]int, 0, n)
	for pos := range n {
		if !erased[pos] {
			candidates = append(candidates, pos)
		}
	}
	for nErrors := 0; 2*nErrors+len(known) <= nParity; nErrors++ {
		var found []int
		forEachCombination(candidates, nErrors, func(extra []int) bool {
			positions := append(append([]int{}, known...), extra...)
			values, ok := solve(synd, positions, n)
			if !ok {
				return true
			}
			// Unknown errors must actually change a symbol, otherwise a smaller
			// pattern would already have matched.
			for i := len(known); i < len(positions); i++ {
				if values[i] == 0 {
					return true
				}
			}
			for i, pos := range positions {
				if values[i] != 0 {
					codeword[pos] ^= values[i]
					found = append(found, pos)
				}
			}
			return false
		})
		if found != nil {
			return found, nil
		}
	}
	return nil, ErrUncorrectable
}

// forEachCombination calls fn with every k-element subset of items, in order, until fn
// returns false.
func forEachCombination(items []int, k int, fn func([]int) bool) {
	combo := make([]int, k)
	var recurse func(start, depth int) bool
	recurse = func(start, depth int) bool {
		if depth == k {
			return fn(combo)
		}
		for i := start; i <= len(items)-(k-depth); i++ {
			combo[depth] = items[i]
			if !recurse(i+1, depth+1) {
				return false
			}
		}
		return true
	}
	recurse(0, 0)
}

// solve finds error values e at positions such that sum(e_k * X_k^j) == synd[j] for
// every j, where X_k = alpha^(n-1-position). It reports false if no values satisfy
// every syndrome.
func solve(synd []byte, positions []int, n int) ([]byte, bool) {
	rows, cols := len(synd), len(positions)
	// Augmented matrix: one row per syndrome equation.
	m := make([][]byte, rows)
	for j := range rows {
		m[j] = make([]byte, cols+1)
		for k, pos := range positions {
			m[j][k] = gf.alphaPow(j * (n - 1 - pos))
		}
		m[j][cols] = synd[j]
	}

	// Gaussian elimination. The columns are distinct Vandermonde columns, so with
	// cols <= rows every column has a pivot.
	for col := range cols {
		pivot := -1
		for r := col; r < rows; r++ {
			if m[r][col] != 0 {
				pivot = r
				break
			}
		}
		if pivot < 0 {
			return nil, false
		}
		m[col], m[pivot] = m[pivot], m[col]
		inv := gf.div(1, m[col][col])
		for c := col; c <= cols; c++ {
			m[col][c] = gf.mul(m[col][c], inv)
		}
		for r := range rows {
			if r != col && m[r][col] != 0 {
				factor := m[r][col]
				for c := col; c <= cols; c++ {
					m[r][c] ^= gf.mul(factor, m[col][c])
				}
			}
		}
	}
	// Remaining equations must be consistent with the solution.
	for r := cols; r < rows; r++ {
		if m[r][cols] != 0 {
			return nil, false
		}
	}
	values := make([]byte, cols)
	for k := range cols {
		values[k] = m[k][cols]
	}
	return values, true
}
