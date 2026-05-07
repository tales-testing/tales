package runtime

import (
	"encoding/binary"
	"hash/fnv"
	"math/rand"
)

// DeriveSeed deterministically derives a new seed from global seed and parts.
func DeriveSeed(globalSeed int64, parts ...string) int64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(globalSeed))
	_, _ = h.Write(buf[:])
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return int64(h.Sum64())
}

// NewDeterministicRand returns deterministic random source.
func NewDeterministicRand(globalSeed int64, parts ...string) *rand.Rand {
	return rand.New(rand.NewSource(DeriveSeed(globalSeed, parts...)))
}
