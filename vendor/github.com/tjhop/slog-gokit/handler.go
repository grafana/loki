package sloggokit

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/go-kit/log"
)

var _ slog.Handler = (*GoKitHandler)(nil)

var defaultGoKitLogger = log.NewLogfmtLogger(os.Stderr)

// Pay boxing cost once at package init, save 3 heap escapes per Handle() call.
var (
	timeKey   any = slog.TimeKey
	msgKey    any = slog.MessageKey
	callerKey any = "caller"
)

// GoKitHandler implements the slog.Handler interface. It holds an internal
// go-kit logger that is used to perform the true logging.
type GoKitHandler struct {
	level        slog.Leveler
	logger       log.Logger
	preformatted []any // pre-flattened key-value pairs, ready to pass directly to logger.Log()
	group        string
}

// NewGoKitHandler returns a new slog logger from the provided go-kit
// logger. Calls to the slog logger are chained to the handler's internal
// go-kit logger. If provided a level, it will be used to filter log events in
// the handler's Enabled() method.
//
// The handler adds a `caller` key to each record, resolved from the program
// counter that slog captured at the log call site. Records handled directly
// (without going through an slog.Logger) have no PC set, and thus omit the
// caller.
func NewGoKitHandler(logger log.Logger, level slog.Leveler) slog.Handler {
	if logger == nil {
		logger = defaultGoKitLogger
	}

	if level == nil {
		level = &slog.LevelVar{} // Info level by default.
	}

	return &GoKitHandler{
		logger: logger,
		level:  level,
	}
}

// Enabled returns true if the internal slog.Leveler is enabled for the
// provided log level. It implements slog.Handler.
func (h *GoKitHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handler take an slog.Record created by an slog.Logger and dispatches the log
// call to the internal go-kit logger. Groups and attributes created by slog
// are formatted and added to the log call as individual key/value pairs. It
// implements slog.Handler.
func (h *GoKitHandler) Handle(_ context.Context, record slog.Record) error {
	// Pre-compute slice capacity. h.preformatted is already flattened to []any
	// key-value pairs at WithAttrs time, so len(h.preformatted) is the exact
	// item count -- no expansion buffer needed for that portion. Record attrs
	// may contain groups that expand beyond 2 items per attr, so include a 50%
	// buffer for that portion's estimated capacity only.
	//
	// We know we need:
	// - 2 for level (key + value)
	// - 2 for caller (key + value)
	// - 2 for timestamp (key + value)
	// - 2 for message (key + value)
	// - len(h.preformatted) exact items (pre-flattened, no expansion)
	// - 2 * record.NumAttrs() for record attrs, +50% buffer for group expansion
	capacity := 8 + len(h.preformatted) + (3 * record.NumAttrs())
	pairs := make([]any, 0, capacity)

	// Append the level directly as the first pair. Matches how the log
	// message is constructed through level.Info()/level.Debug()/etc
	// wrappers, but without the extra allocation/copy per call.
	pairs = append(pairs, levelKey, gokitLevelValue(record.Level))

	// Resolve the log call site from the PC that slog captured when the
	// record was created. Cheaper and more accurate to do it here since
	// it's already captured, vs relying on setting `log.Caller()` depth
	// and hoping it unwinds correctly.
	if record.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{record.PC})
		f, _ := fs.Next()
		if f.File != "" {
			// Trim to basename:line with the same logic as
			// go-kit's log.Caller:
			// https://github.com/go-kit/log/blob/v0.2.1/value.go#L84-L93
			// 
			// path.Base() would work, opting for direct compatibility.
			idx := strings.LastIndexByte(f.File, '/')
			pairs = append(pairs, callerKey, f.File[idx+1:]+":"+strconv.Itoa(f.Line))
		}
	}

	if !record.Time.IsZero() {
		pairs = append(pairs, timeKey, record.Time)
	}
	pairs = append(pairs, msgKey, record.Message)

	// Bulk-append pre-flattened attrs, group prefixes were resolved at
	// WithAttrs() call.
	pairs = append(pairs, h.preformatted...)

	record.Attrs(func(a slog.Attr) bool {
		pairs = appendPair(pairs, h.group, a)
		return true
	})

	return h.logger.Log(pairs...)
}

// WithAttrs formats the provided attributes and caches them in the handler to
// attach to all future log calls. It implements slog.Handler.
func (h *GoKitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Make a defensive copy of preformatted attrs to avoid race conditions
	// when multiple goroutines call WithAttrs concurrently on the same handler.
	// Attrs are pre-flattened to []any key-value pairs here so that Handle()
	// can bulk-copy them without per-attr processing on every log call.
	//
	// Capacity estimate: existing items + 2 per new attr (minimum, more if
	// attrs contain groups that expand to multiple pairs).
	pairs := make([]any, len(h.preformatted), len(h.preformatted)+(len(attrs)*2))
	copy(pairs, h.preformatted)

	for _, attr := range attrs {
		pairs = appendPair(pairs, h.group, attr)
	}

	return &GoKitHandler{
		logger:       h.logger,
		level:        h.level,
		preformatted: pairs,
		group:        h.group,
	}
}

// WithGroup sets the group prefix string and caches it within the handler to
// use to prefix all future log attribute pairs. It implements slog.Handler.
func (h *GoKitHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	g := name
	if h.group != "" {
		g = h.group + "." + g
	}

	return &GoKitHandler{
		logger:       h.logger,
		level:        h.level,
		preformatted: h.preformatted,
		group:        g,
	}
}

func appendPair(pairs []any, groupPrefix string, attr slog.Attr) []any {
	if attr.Equal(slog.Attr{}) {
		return pairs
	}

	switch attr.Value.Kind() {
	case slog.KindGroup:
		attrs := attr.Value.Group()
		if len(attrs) > 0 {
			// Only process groups that have non-empty attributes
			// to properly conform to slog.Handler interface
			// contract.

			if attr.Key != "" {
				// If a group's key is empty, attributes should
				// be inlined to properly conform to
				// slog.Handler interface contract.
				if groupPrefix != "" {
					groupPrefix = groupPrefix + "." + attr.Key
				} else {
					groupPrefix = attr.Key
				}
			}
			for _, a := range attrs {
				pairs = appendPair(pairs, groupPrefix, a)
			}
		}
	default:
		key := attr.Key
		if groupPrefix != "" {
			key = groupPrefix + "." + key
		}

		pairs = append(pairs, key, attr.Value.Resolve())
	}

	return pairs
}
