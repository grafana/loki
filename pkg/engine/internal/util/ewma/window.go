package ewma

import (
	"math"
	"strings"
	"time"
)

// window represents a window size for EWMA calculations; such as a 15m window.
type window struct {
	Size time.Duration

	initialized bool
	value       float64
	lastUpdate  time.Time
}

// Name returns a name for the window, based on its size. Unlike
// [time.Duration.String], trailing zero units are removed, so 15m0s becomes
// 15m.
func (w *window) Name() string {
	name := w.Size.String()

	if strings.HasSuffix(name, "m0s") {
		name = name[:len(name)-2] // Trim 0s
	}
	if strings.HasSuffix(name, "h0m") {
		name = name[:len(name)-2] // Trim 0m
	}
	return name
}

// Observe updates the window with a new value. Observe reinitializes the window
// the now timestamp is earlier than now timestamp on the previous call.
func (w *window) Observe(value float64, now time.Time) {
	// We'll also treat clock drift as reinitialization.
	if !w.initialized || now.Before(w.lastUpdate) {
		w.initialized = true
		w.value = value
		w.lastUpdate = now
		return
	}

	// EWMA is calculated using the formula:
	//   ewma_new = decay * ewma_old + (1 - decay) * value
	//
	// Where decay is:
	//   e^(-delta/window_size)

	delta := now.Sub(w.lastUpdate)
	decay := math.Exp(-delta.Seconds() / w.Size.Seconds())

	w.value = decay*w.value + (1-decay)*value
	w.lastUpdate = now
}

// Value returns the current EWMA value.
func (w *window) Value() float64 { return w.value }
