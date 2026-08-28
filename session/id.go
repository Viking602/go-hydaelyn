package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// IDGenerator mints monotonic UUIDv7 identifiers.
type IDGenerator struct {
	mu      sync.Mutex
	lastMs  int64
	lastSeq uint16
}

func NewIDGenerator() *IDGenerator {
	return &IDGenerator{}
}

// Next mints a UUIDv7. With no args, the timestamp is now. One extra arg
// requests the unix-millisecond field, which is how follower ids are pinned
// behind the entry they belong to; further args are ignored. The request is a
// floor, not an override: a value older than the last minted millisecond is
// clamped forward, because a follower that dragged the generator backwards
// would cost every later id its ordering. Uniqueness rests on the random bits
// either way.
func (g *IDGenerator) Next(timestampMs ...int64) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	ms := time.Now().UnixMilli()
	if len(timestampMs) > 0 {
		ms = timestampMs[0]
	}
	if ms < g.lastMs {
		// A pinned follower, or a wall clock that stepped backwards: keep
		// minting forward from the last millisecond used so ids stay sortable.
		ms = g.lastMs
	}
	var seq uint16
	if ms == g.lastMs {
		seq = g.lastSeq + 1
		if seq > 0x0FFF {
			ms++
			seq = 0
		}
	}
	g.lastMs = ms
	g.lastSeq = seq

	var randB [8]byte
	// Go 1.24+ guarantees crypto/rand.Read never returns an error.
	_, _ = rand.Read(randB[:])

	var uuid [16]byte
	uuid[0] = byte(ms >> 40)
	uuid[1] = byte(ms >> 32)
	uuid[2] = byte(ms >> 24)
	uuid[3] = byte(ms >> 16)
	uuid[4] = byte(ms >> 8)
	uuid[5] = byte(ms)
	uuid[6] = 0x70 | byte(seq>>8)
	uuid[7] = byte(seq)
	copy(uuid[8:], randB[:])
	uuid[8] = (uuid[8] & 0x3F) | 0x80

	return formatUUID(uuid)
}

// TimestampMs returns the unix-millisecond field of a UUIDv7 minted by
// IDGenerator. It reports false for anything not in that layout so callers can
// fall back to wall-clock time instead of pinning follower ids to epoch zero.
func TimestampMs(id string) (int64, bool) {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return 0, false
	}
	var digits [32]byte
	next := 0
	for index := range len(id) {
		switch index {
		case 8, 13, 18, 23:
			continue
		default:
			digits[next] = id[index]
			next++
		}
	}
	var uuid [16]byte
	if _, err := hex.Decode(uuid[:], digits[:]); err != nil {
		return 0, false
	}
	if uuid[6]>>4 != 7 || uuid[8]&0xC0 != 0x80 {
		return 0, false
	}
	ms := int64(uuid[0])<<40 |
		int64(uuid[1])<<32 |
		int64(uuid[2])<<24 |
		int64(uuid[3])<<16 |
		int64(uuid[4])<<8 |
		int64(uuid[5])
	return ms, true
}

func formatUUID(uuid [16]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], uuid[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], uuid[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], uuid[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], uuid[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], uuid[10:16])
	return string(buf[:])
}
