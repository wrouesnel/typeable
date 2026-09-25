// Package crockford converts between bytes and 5-bit symbols written with Crockford's
// base32 alphabet, which avoids letters that are easily confused when read or typed.
package crockford

import "strings"

// Alphabet maps each 5-bit symbol to its character.
const Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Char returns the character for a symbol, which must be less than 32.
func Char(symbol byte) byte {
	return Alphabet[symbol]
}

// Symbol returns the symbol for a typed character. It is case-insensitive and accepts
// the common substitutions O for 0 and I or L for 1.
func Symbol(r rune) (byte, bool) {
	switch r {
	case 'o', 'O':
		return 0, true
	case 'i', 'I', 'l', 'L':
		return 1, true
	}
	if r >= 'a' && r <= 'z' {
		r -= 'a' - 'A'
	}
	idx := strings.IndexRune(Alphabet, r)
	if idx < 0 {
		return 0, false
	}
	return byte(idx), true
}

// FromBytes splits data into 5-bit symbols, most significant bit first. The final
// symbol is padded with zero bits.
func FromBytes(data []byte) []byte {
	symbols := make([]byte, 0, (len(data)*8+4)/5)
	var acc uint
	var bits uint
	for _, b := range data {
		acc = acc<<8 | uint(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			symbols = append(symbols, byte(acc>>bits)&0x1f)
		}
	}
	if bits > 0 {
		symbols = append(symbols, byte(acc<<(5-bits))&0x1f)
	}
	return symbols
}

// ToBytes packs 5-bit symbols back into bytes, discarding any trailing partial byte.
func ToBytes(symbols []byte) []byte {
	data := make([]byte, 0, len(symbols)*5/8)
	var acc uint
	var bits uint
	for _, s := range symbols {
		acc = acc<<5 | uint(s&0x1f)
		bits += 5
		if bits >= 8 {
			bits -= 8
			data = append(data, byte(acc>>bits))
		}
	}
	return data
}
