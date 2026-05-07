package runtime

import "testing"

func TestDeriveSeedDeterministic(t *testing.T) {
	t.Parallel()
	seedA := DeriveSeed(1234, "scenario", "step", "generator")
	seedB := DeriveSeed(1234, "scenario", "step", "generator")
	seedC := DeriveSeed(1234, "scenario", "other", "generator")

	if seedA != seedB {
		t.Fatalf("same parts must produce same seed")
	}
	if seedA == seedC {
		t.Fatalf("different parts should produce different seeds")
	}
}

func TestNewDeterministicRandStableSequence(t *testing.T) {
	t.Parallel()
	r1 := NewDeterministicRand(999, "x")
	r2 := NewDeterministicRand(999, "x")
	for i := 0; i < 10; i++ {
		if r1.Intn(1000000) != r2.Intn(1000000) {
			t.Fatalf("deterministic sequence mismatch at %d", i)
		}
	}
}
