package runtime

import (
	"fmt"
	"hash/fnv"
	"math"
	randv2 "math/rand/v2"
)

// DeterministicRand is a deterministic pseudo-random generator.
type DeterministicRand struct {
	state uint64
}

// Intn returns a deterministic pseudo-random integer in [0,n).
func (d *DeterministicRand) Intn(n int) int {
	if n <= 0 {
		return 0
	}

	d.state ^= d.state >> 12
	d.state ^= d.state << 25
	d.state ^= d.state >> 27
	mixed := d.state * 2685821657736338717

	value := int64(mixed & math.MaxInt64)
	result := value % int64(n)

	return int(result)
}

// DeriveSeed deterministically derives a new seed from global seed and parts.
func DeriveSeed(globalSeed int64, parts ...string) uint64 {
	h := fnv.New64a()

	_, _ = fmt.Fprintf(h, "%d", globalSeed)

	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}

	return h.Sum64()
}

// NewDeterministicRand returns deterministic random source.
func NewDeterministicRand(globalSeed int64, parts ...string) *DeterministicRand {
	seed := DeriveSeed(globalSeed, parts...)
	if seed == 0 {
		seed = 1
	}

	return &DeterministicRand{state: seed}
}

// NewDeterministicRandV2 returns a deterministic math/rand/v2 source.
func NewDeterministicRandV2(globalSeed int64, parts ...string) *randv2.Rand {
	seed1 := DeriveSeed(globalSeed, parts...)
	seedParts2 := append(append([]string(nil), parts...), "randv2")

	seed2 := DeriveSeed(globalSeed, seedParts2...)
	if seed1 == 0 && seed2 == 0 {
		seed2 = 1
	}

	//nolint:gosec // Deterministic test data requires seeded pseudo-randomness.
	return randv2.New(randv2.NewPCG(seed1, seed2))
}
