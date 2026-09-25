package crockford_test

import (
	"bytes"
	"testing"

	"github.com/wrouesnel/typeable/pkg/crockford"
)

func TestRoundTrip(t *testing.T) {
	for n := range 20 {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i*37 + n)
		}
		got := crockford.ToBytes(crockford.FromBytes(data))
		if !bytes.Equal(got, data) {
			t.Fatalf("len %d: got %x, want %x", n, got, data)
		}
	}
}

func TestSymbolIsLenient(t *testing.T) {
	cases := map[rune]byte{'0': 0, 'o': 0, 'O': 0, 'i': 1, 'L': 1, 'a': 10, 'z': 31, 'Z': 31}
	for r, want := range cases {
		got, ok := crockford.Symbol(r)
		if !ok || got != want {
			t.Errorf("Symbol(%q) = %d, %v; want %d", r, got, ok, want)
		}
	}
	for _, r := range "uU-!" {
		if _, ok := crockford.Symbol(r); ok {
			t.Errorf("Symbol(%q) accepted", r)
		}
	}
}

func TestCharMatchesSymbol(t *testing.T) {
	for s := range byte(32) {
		got, ok := crockford.Symbol(rune(crockford.Char(s)))
		if !ok || got != s {
			t.Errorf("symbol %d did not round trip", s)
		}
	}
}
