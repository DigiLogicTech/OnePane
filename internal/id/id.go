package id

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// Generator creates opaque, time-sortable UUIDv7 identifiers with an optional
// stable domain prefix (for example: task_<uuid>). The UUID generation uses
// cryptographic randomness and RFC 9562 version/variant bits.
type Generator struct{}

func (Generator) New(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate random id bytes: %w", err)
	}

	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	// RFC 9562 UUIDv7 version and RFC 4122 variant.
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80

	var raw [36]byte
	hex.Encode(raw[0:8], b[0:4])
	raw[8] = '-'
	hex.Encode(raw[9:13], b[4:6])
	raw[13] = '-'
	hex.Encode(raw[14:18], b[6:8])
	raw[18] = '-'
	hex.Encode(raw[19:23], b[8:10])
	raw[23] = '-'
	hex.Encode(raw[24:36], b[10:16])

	if prefix == "" {
		return string(raw[:]), nil
	}
	return prefix + "_" + string(raw[:]), nil
}

// TimestampMillis extracts the UUIDv7 millisecond timestamp from a raw UUID
// byte array. It is intentionally kept package-private for future tests/tools.
func timestampMillis(b [16]byte) uint64 {
	return uint64(binary.BigEndian.Uint16(b[0:2]))<<32 |
		uint64(binary.BigEndian.Uint32(b[2:6]))
}
