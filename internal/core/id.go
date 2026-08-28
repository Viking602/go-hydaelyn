package core

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Generated IDs are written into a store that outlives the Runtime — and the
// process — that opened it. A counter cannot be the source of uniqueness
// here: after a crash a second Runtime resumes against the same store, and a
// counter that restarts re-mints IDs the store already holds. That collision
// is silent and damaging, because a fresh lease acquisition whose ID matches
// an abandoned lease fails its expected-version CAS and reports the stale
// lease as still active.
//
// runtimeIDs therefore mints UUIDv7: 48 bits of unix-millisecond timestamp,
// a 12-bit intra-millisecond sequence, and 62 bits from crypto/rand. Nothing
// about it depends on process-local state surviving, so IDs stay unique
// across restarts and across hosts writing to one store. The layout also
// sorts by mint time, which keeps ID order useful for debugging.
var runtimeIDs idGenerator

func (r *Runtime) newID(prefix string) string {
	return prefix + "-" + runtimeIDs.next()
}

// idGenerator mints monotonic UUIDv7 values. The sequence counter only orders
// IDs minted within the same millisecond; uniqueness rests on the random bits.
type idGenerator struct {
	mu      sync.Mutex
	lastMs  int64
	lastSeq uint16
}

func (g *idGenerator) next() string {
	g.mu.Lock()
	ms := time.Now().UnixMilli()
	var seq uint16
	if ms < g.lastMs {
		// The wall clock went backwards; keep minting forward from the last
		// millisecond we used so IDs stay sortable.
		ms = g.lastMs
	}
	if ms == g.lastMs {
		seq = g.lastSeq + 1
		if seq > 0x0FFF {
			ms++
			seq = 0
		}
	}
	g.lastMs = ms
	g.lastSeq = seq
	g.mu.Unlock()

	var random [8]byte
	// Go 1.24+ guarantees crypto/rand.Read never returns an error.
	_, _ = rand.Read(random[:])

	var uuid [16]byte
	uuid[0] = byte(ms >> 40)
	uuid[1] = byte(ms >> 32)
	uuid[2] = byte(ms >> 24)
	uuid[3] = byte(ms >> 16)
	uuid[4] = byte(ms >> 8)
	uuid[5] = byte(ms)
	uuid[6] = 0x70 | byte(seq>>8) // version 7
	uuid[7] = byte(seq)
	copy(uuid[8:], random[:])
	uuid[8] = (uuid[8] & 0x3F) | 0x80 // RFC 4122 variant

	return formatUUID(uuid)
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
