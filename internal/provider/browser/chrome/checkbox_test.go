package chrome

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAwaitCheckStateWaitsForTheWantedState(t *testing.T) {
	t.Parallel()

	answers := []checkState{
		{Found: false},
		{Found: true, Checked: false},
		{Found: true, Checked: true},
	}

	calls := 0
	read := func(context.Context) (checkState, error) {
		state := answers[min(calls, len(answers)-1)]
		calls++

		return state, nil
	}

	state, err := awaitCheckState(context.Background(), time.Millisecond, read, func(s checkState) bool { return s.Found && s.Checked })
	if err != nil || !state.Checked {
		t.Fatalf("awaitCheckState = %+v, %v; want the checked state", state, err)
	}

	if calls != 3 {
		t.Fatalf("reads = %d, want 3", calls)
	}
}

func TestAwaitCheckStateGivesUpAtTheDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	read := func(context.Context) (checkState, error) { return checkState{Found: true}, nil }

	state, err := awaitCheckState(ctx, time.Millisecond, read, func(s checkState) bool { return s.Checked })
	if !errors.Is(err, errStateUnchanged) {
		t.Fatalf("err = %v, want errStateUnchanged", err)
	}

	if !state.Found || state.Checked {
		t.Fatalf("state = %+v, want the last state read", state)
	}
}

func TestAwaitCheckStateStopsOnANonCheckbox(t *testing.T) {
	t.Parallel()

	calls := 0
	read := func(context.Context) (checkState, error) {
		calls++

		return checkState{Fatal: "element is not a checkbox or a radio button"}, nil
	}

	state, err := awaitCheckState(context.Background(), time.Millisecond, read, func(checkState) bool { return true })
	if err == nil || state.Fatal == "" || calls != 1 {
		t.Fatalf("awaitCheckState = %+v, %v after %d reads; want an immediate failure", state, err, calls)
	}
}

func TestAwaitCheckStateRetriesAFailedRead(t *testing.T) {
	t.Parallel()

	calls := 0
	read := func(context.Context) (checkState, error) {
		calls++
		if calls == 1 {
			return checkState{}, errors.New("Execution context was destroyed")
		}

		return checkState{Found: true}, nil
	}

	if _, err := awaitCheckState(context.Background(), time.Millisecond, read, func(s checkState) bool { return s.Found }); err != nil {
		t.Fatalf("awaitCheckState: %v", err)
	}
}
