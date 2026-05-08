package limitedreader

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestSingleReader_UnderBudget(t *testing.T) {
	p := NewPool(1024)
	r := p.NewReader(strings.NewReader("hello world"))
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestSingleReader_OverBudget(t *testing.T) {
	p := NewPool(4)
	r := p.NewReader(bytes.NewReader([]byte("hello world")))
	defer r.Close()

	// 8-byte buffer triggers a single Read of up to 8 bytes; budget is 4.
	buf := make([]byte, 8)
	_, err := r.Read(buf)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("got err %v, want ErrBudgetExceeded", err)
	}

	// Subsequent reads stay sticky-erroring.
	_, err = r.Read(buf)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("second Read err %v, want ErrBudgetExceeded", err)
	}
}

func TestParallel_CombinedExceedsBudget(t *testing.T) {
	// Budget is 8 bytes total. Two readers each want 6 bytes — only one
	// can succeed; the other must hit ErrBudgetExceeded.
	p := NewPool(8)
	src := func() io.Reader { return strings.NewReader("AAAAAA") } // 6 bytes

	r1 := p.NewReader(src())
	r2 := p.NewReader(src())
	defer r1.Close()
	defer r2.Close()

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, r := range []*Reader{r1, r2} {
		wg.Add(1)
		go func(idx int, r *Reader) {
			defer wg.Done()
			_, err := io.ReadAll(r)
			results[idx] = err
		}(i, r)
	}
	wg.Wait()

	var ok, exceeded int
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrBudgetExceeded):
			exceeded++
		default:
			t.Fatalf("unexpected err: %v", err)
		}
	}
	if ok != 1 || exceeded != 1 {
		t.Fatalf("got ok=%d exceeded=%d, want ok=1 exceeded=1", ok, exceeded)
	}
}

func TestClose_ReleasesBudget(t *testing.T) {
	// Budget is 6. First reader uses all 6, closes, then a second reader
	// can use 6 again.
	p := NewPool(6)

	r1 := p.NewReader(strings.NewReader("AAAAAA"))
	if _, err := io.ReadAll(r1); err != nil {
		t.Fatalf("r1 ReadAll: %v", err)
	}
	if err := r1.Close(); err != nil {
		t.Fatalf("r1 Close: %v", err)
	}

	r2 := p.NewReader(strings.NewReader("BBBBBB"))
	defer r2.Close()
	got, err := io.ReadAll(r2)
	if err != nil {
		t.Fatalf("r2 ReadAll: %v", err)
	}
	if string(got) != "BBBBBB" {
		t.Fatalf("r2 got %q, want %q", got, "BBBBBB")
	}
}

func TestSingleReader_UpToFullBudget(t *testing.T) {
	// A single reader is allowed to use the entire budget.
	p := NewPool(1024)
	body := strings.Repeat("x", 1024)
	r := p.NewReader(strings.NewReader(body))
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 1024 {
		t.Fatalf("got %d bytes, want 1024", len(got))
	}
}
