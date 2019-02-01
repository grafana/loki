package tomb_test

import (
	"errors"
	"gopkg.in/tomb.v1"
	"reflect"
	"testing"
)

func TestNewTomb(t *testing.T) {
	tb := &tomb.Tomb{}
	testState(t, tb, false, false, tomb.ErrStillAlive)

	tb.Done()
	testState(t, tb, true, true, nil)
}

func TestKill(t *testing.T) {
	// a nil reason flags the goroutine as dying
	tb := &tomb.Tomb{}
	tb.Kill(nil)
	testState(t, tb, true, false, nil)

	// a non-nil reason now will override Kill
	err := errors.New("some error")
	tb.Kill(err)
	testState(t, tb, true, false, err)

	// another non-nil reason won't replace the first one
	tb.Kill(errors.New("ignore me"))
	testState(t, tb, true, false, err)

	tb.Done()
	testState(t, tb, true, true, err)
}

func TestKillf(t *testing.T) {
	tb := &tomb.Tomb{}

	err := tb.Killf("BO%s", "OM")
	if s := err.Error(); s != "BOOM" {
		t.Fatalf(`Killf("BO%s", "OM"): want "BOOM", got %q`, s)
	}
	testState(t, tb, true, false, err)

	// another non-nil reason won't replace the first one
	tb.Killf("ignore me")
	testState(t, tb, true, false, err)

	tb.Done()
	testState(t, tb, true, true, err)
}

func TestErrDying(t *testing.T) {
	// ErrDying being used properly, after a clean death.
	tb := &tomb.Tomb{}
	tb.Kill(nil)
	tb.Kill(tomb.ErrDying)
	testState(t, tb, true, false, nil)

	// ErrDying being used properly, after an errorful death.
	err := errors.New("some error")
	tb.Kill(err)
	tb.Kill(tomb.ErrDying)
	testState(t, tb, true, false, err)

	// ErrDying being used badly, with an alive tomb.
	tb = &tomb.Tomb{}
	defer func() {
		err := recover()
		if err != "tomb: Kill with ErrDying while still alive" {
			t.Fatalf("Wrong panic on Kill(ErrDying): %v", err)
		}
		testState(t, tb, false, false, tomb.ErrStillAlive)
	}()
	tb.Kill(tomb.ErrDying)
}

func testState(t *testing.T, tb *tomb.Tomb, wantDying, wantDead bool, wantErr error) {
	select {
	case <-tb.Dying():
		if !wantDying {
			t.Error("<-Dying: should block")
		}
	default:
		if wantDying {
			t.Error("<-Dying: should not block")
		}
	}
	seemsDead := false
	select {
	case <-tb.Dead():
		if !wantDead {
			t.Error("<-Dead: should block")
		}
		seemsDead = true
	default:
		if wantDead {
			t.Error("<-Dead: should not block")
		}
	}
	if err := tb.Err(); err != wantErr {
		t.Errorf("Err: want %#v, got %#v", wantErr, err)
	}
	if wantDead && seemsDead {
		waitErr := tb.Wait()
		switch {
		case waitErr == tomb.ErrStillAlive:
			t.Errorf("Wait should not return ErrStillAlive")
		case !reflect.DeepEqual(waitErr, wantErr):
			t.Errorf("Wait: want %#v, got %#v", wantErr, waitErr)
		}
	}
}
