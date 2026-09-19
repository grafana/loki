// Package metrictimer provides a small helper for timing named operations.
package metrictimer

import "time"

// Time runs fn and returns how long it took along with fn's error. It is the
// single-observation counterpart to [Timer]: use it at sites that time exactly
// one call and record a single duration, rather than partitioning an operation
// into named phases. The caller records the returned duration into its own
// histogram.
func Time(fn func() error) (time.Duration, error) {
	start := time.Now()
	err := fn()
	return time.Since(start), err
}

// Phase is a bounded metric phase label value.
type Phase string

// String returns p as a metric label value.
func (p Phase) String() string { return string(p) }

// Outcome is a bounded metric outcome label value.
type Outcome string

// String returns o as a metric label value.
func (o Outcome) String() string { return string(o) }

// Config configures a Timer.
type Config struct {
	// Emit records one duration row. Required.
	Emit func(Phase, Outcome, time.Duration)

	// Total is the phase value to emit for the operation total when HasTotal is true.
	Total Phase

	// HasTotal controls whether Done emits a total row.
	HasTotal bool

	// Start is the operation start time. Defaults to time.Now().
	Start time.Time

	// Rows is the initial capacity for buffered rows. Defaults to 4.
	Rows int
}

// Timer times one operation as a sequence of named phases plus an optional
// total, and flushes all observations when the operation ends.
//
// Phases are named at the start of their interval: Phase closes the previous
// interval and opens p; Done closes the final open interval. Observe records a
// discrete duration measured by another resource. A nil *Timer is a no-op.
//
// Within one operation, either partition time with Phase or record discrete
// durations with Observe/Region; do not call Observe or Region while a Phase
// interval is open, or that time is counted twice (once in the row and once in
// the open phase).
type Timer struct {
	emit func(Phase, Outcome, time.Duration)

	total    Phase
	hasTotal bool

	start   time.Time
	open    time.Time
	cur     Phase
	inPhase bool

	rows []row
}

type row struct {
	phase Phase
	dur   time.Duration
}

// New creates a Timer from cfg. It returns nil when cfg.Emit is nil.
func New(cfg Config) *Timer {
	if cfg.Emit == nil {
		return nil
	}
	if cfg.Start.IsZero() {
		cfg.Start = time.Now()
	}
	if cfg.Rows <= 0 {
		cfg.Rows = 4
	}
	return &Timer{
		emit:     cfg.Emit,
		total:    cfg.Total,
		hasTotal: cfg.HasTotal,
		start:    cfg.Start,
		rows:     make([]row, 0, cfg.Rows),
	}
}

// Phase closes the currently-open interval and opens p.
func (t *Timer) Phase(p Phase) {
	if t == nil || t.emit == nil {
		return
	}

	now := time.Now()
	if t.inPhase {
		t.rows = append(t.rows, row{phase: t.cur, dur: now.Sub(t.open)})
	}
	t.cur = p
	t.open = now
	t.inPhase = true
}

// Observe records d for p without disturbing the currently-open interval.
func (t *Timer) Observe(p Phase, d time.Duration) {
	if t == nil || t.emit == nil || d < 0 {
		return
	}
	t.rows = append(t.rows, row{phase: p, dur: d})
}

// Region runs fn and records the elapsed time for p. Region does not disturb
// the currently-open interval.
func (t *Timer) Region(p Phase, fn func() error) error {
	if t == nil || t.emit == nil {
		return fn()
	}

	start := time.Now()
	err := fn()
	t.rows = append(t.rows, row{phase: p, dur: time.Since(start)})
	return err
}

// Done closes the final open interval, emits all buffered rows with out, and
// emits the optional total row. Done is idempotent: it renders t inert, so a
// second call (or a stray Phase/Observe/Region afterward) is a no-op.
func (t *Timer) Done(out Outcome) {
	if t == nil || t.emit == nil {
		return
	}

	now := time.Now()
	if t.inPhase {
		t.rows = append(t.rows, row{phase: t.cur, dur: now.Sub(t.open)})
	}
	if t.hasTotal {
		t.rows = append(t.rows, row{phase: t.total, dur: now.Sub(t.start)})
	}

	for _, row := range t.rows {
		t.emit(row.phase, out, row.dur)
	}

	t.emit = nil
	t.inPhase = false
	t.cur = ""
	t.open = time.Time{}
	t.start = time.Time{}
	t.rows = t.rows[:0]
}
