package rs32_test

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/wrouesnel/typeable/pkg/rs32"
)

func randomCodeword(t *testing.T, r *rand.Rand, nData, nParity int) []byte {
	t.Helper()
	data := make([]byte, nData)
	for i := range data {
		data[i] = byte(r.IntN(32))
	}
	parity, err := rs32.Encode(data, nParity)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, parity...)
}

// corrupt changes the symbols at positions to different random values.
func corrupt(r *rand.Rand, codeword []byte, positions []int) {
	for _, pos := range positions {
		codeword[pos] ^= byte(1 + r.IntN(31))
	}
}

func TestCleanCodewordDecodes(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	codeword := randomCodeword(t, r, 18, 3)
	corrected, err := rs32.Decode(codeword, 3, nil)
	if err != nil || corrected != nil {
		t.Fatalf("got %v, %v; want no corrections", corrected, err)
	}
}

func TestCorrectsWithinCapacity(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	cases := []struct {
		name     string
		nParity  int
		errors   int
		erasures int
	}{
		{"one error, two parity", 2, 1, 0},
		{"two erasures, two parity", 2, 0, 2},
		{"one error one erasure, three parity", 3, 1, 1},
		{"three erasures, three parity", 3, 0, 3},
		{"two errors, five parity", 5, 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for range 200 {
				original := randomCodeword(t, r, 17, tc.nParity)
				n := len(original)
				positions := r.Perm(n)[:tc.errors+tc.erasures]
				received := slices.Clone(original)
				corrupt(r, received, positions)
				erasures := positions[tc.errors:]

				corrected, err := rs32.Decode(received, tc.nParity, erasures)
				if err != nil {
					t.Fatalf("decode failed: %v", err)
				}
				if !slices.Equal(received, original) {
					t.Fatalf("got %v, want %v", received, original)
				}
				if len(corrected) != len(positions) {
					t.Fatalf("corrected %v, want %d positions", corrected, len(positions))
				}
			}
		})
	}
}

func TestTooManyErrorsNotSilentlyAccepted(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	original := randomCodeword(t, r, 18, 2)
	received := slices.Clone(original)
	corrupt(r, received, []int{0, 1, 2})
	_, err := rs32.Decode(received, 2, []int{0, 1, 2})
	if !errors.Is(err, rs32.ErrUncorrectable) {
		t.Fatalf("got %v, want ErrUncorrectable", err)
	}
}

func TestRejectsBadInput(t *testing.T) {
	if _, err := rs32.Encode(make([]byte, 30), 2); !errors.Is(err, rs32.ErrInvalidLength) {
		t.Errorf("overlong codeword: got %v", err)
	}
	if _, err := rs32.Encode([]byte{32}, 2); !errors.Is(err, rs32.ErrInvalidSymbol) {
		t.Errorf("bad symbol: got %v", err)
	}
}
