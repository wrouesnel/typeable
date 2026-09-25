package codec

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

const checksumSize = 4

// ErrChecksum is returned when decoded data doesn't match its checksum, meaning errors
// were miscorrected or the grid was typed wrongly in a way parity couldn't detect.
var ErrChecksum = errors.New("checksum mismatch: the decoded data is corrupt")

// compress frames plaintext as a CRC-32 of the plaintext followed by a raw deflate
// stream. Deflate streams are self-terminating, so padding after it is ignored.
func compress(plaintext []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write(binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(plaintext)))
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decompress reverses compress and verifies the checksum.
func decompress(payload []byte) ([]byte, error) {
	if len(payload) < checksumSize {
		return nil, fmt.Errorf("%w: payload too short", ErrChecksum)
	}
	want := binary.BigEndian.Uint32(payload)
	plaintext, err := io.ReadAll(flate.NewReader(bytes.NewReader(payload[checksumSize:])))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrChecksum, err)
	}
	if crc32.ChecksumIEEE(plaintext) != want {
		return nil, ErrChecksum
	}
	return plaintext, nil
}
