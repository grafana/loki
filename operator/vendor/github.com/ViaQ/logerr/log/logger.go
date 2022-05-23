package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ViaQ/logerr/internal/kv"
	"github.com/ViaQ/logerr/kverrors"
	"github.com/go-logr/logr"
)

// Keys used to log specific builtin fields
const (
	TimeStampKey = "_ts"
	FileLineKey  = "_file:line"
	LevelKey     = "_level"
	ComponentKey = "_component"
	MessageKey   = "_message"
	ErrorKey     = "_error"
)

// Line orders log line fields
type Line struct {
	Timestamp string
	FileLine  string
	Verbosity string
	Component string
	Message   string
	Context   map[string]interface{}
}

// LineJSON add json tags to Line struct (production logs)
type LineJSON struct {
	Timestamp string                 `json:"_ts"`
	FileLine  string                 `json:"-"`
	Verbosity string                 `json:"_level"`
	Component string                 `json:"_component"`
	Message   string                 `json:"_message"`
	Context   map[string]interface{} `json:"-"`
}

// LineJSONDev add json tags to Line struct (developer logs, enable using environment variable LOG_DEV)
type LineJSONDev struct {
	Timestamp string                 `json:"_ts"`
	FileLine  string                 `json:"_file:line"`
	Verbosity string                 `json:"_level"`
	Component string                 `json:"_component"`
	Message   string                 `json:"_message"`
	Context   map[string]interface{} `json:"-"`
}

// MarshalJSON implements custom marshaling for log line: (1) flattening context (2) support for developer mode
func (l Line) MarshalJSON() ([]byte, error) {
	lineTemp := LineJSON(l)

	lineValue, _ := json.Marshal(lineTemp)
	verbosity, errConvert := strconv.Atoi(l.Verbosity)
	if verbosity > 1 && errConvert == nil {
		lineTempDev := LineJSONDev(l)
		lineValue, _ = json.Marshal(lineTempDev)
	}
	lineValue = lineValue[1 : len(lineValue)-1]

	contextValue, _ := json.Marshal(lineTemp.Context)
	contextValue = contextValue[1 : len(contextValue)-1]

	sep := ""
	if len(contextValue) > 0 {
		sep = ","
	}
	return []byte(fmt.Sprintf("{%s%s%s}", lineValue, sep, contextValue)), nil
}

// Verbosity is a level of verbosity to log between 0 and math.MaxInt32
// However it is recommended to keep the numbers between 0 and 3
type Verbosity int

func (v Verbosity) String() string {
	return strconv.Itoa(int(v))
}

// MarshalJSON marshals JSON
func (v Verbosity) MarshalJSON() ([]byte, error) {
	return []byte(v.String()), nil
}

// TimestampFunc returns a string formatted version of the current time.
// This should probably only be used with tests or if you want to change
// the default time formatting of the output logs.
var TimestampFunc = func() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// Logger writes logs to a specified output
type Logger struct {
	mtx       sync.RWMutex
	verbosity Verbosity
	output    io.Writer
	context   map[string]interface{}
	encoder   Encoder
	name      string
}

// NewLogger creates a new logger
func NewLogger(name string, w io.Writer, v Verbosity, e Encoder, keysAndValues ...interface{}) *Logger {
	return &Logger{
		name:      name,
		verbosity: v,
		output:    w,
		context:   kv.ToMap(keysAndValues...),
		encoder:   e,
	}
}

// combine creates a new map combining context and keysAndValues.
func combine(context map[string]interface{}, keysAndValues ...interface{}) map[string]interface{} {
	nc := make(map[string]interface{}, len(context)+len(keysAndValues)/2)
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			key, ok := keysAndValues[i].(string) // It should be a string.
			if !ok {                             // But this is not the place to panic
				key = fmt.Sprintf("%s", keysAndValues[i]) // So use this expensive conversion instead.
			}
			nc[key] = keysAndValues[i+1]
		}
	}
	for k, v := range context {
		nc[k] = v
	}

	return nc
}

// withValues clones the logger and appends keysAndValues
// but returns a struct instead of the logr.Logger interface
func (l *Logger) withValues(keysAndValues ...interface{}) *Logger {
	ll := NewLogger(l.name, l.output, l.verbosity, l.encoder)
	ll.context = combine(l.context, keysAndValues...)
	return ll
}

// WithValues clones the logger and appends keysAndValues
func (l *Logger) WithValues(keysAndValues ...interface{}) logr.Logger {
	return l.withValues(keysAndValues...)
}

// SetOutput sets the writer that JSON is written to
func (l *Logger) SetOutput(w io.Writer) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.output = w
}

// Enabled tests whether this Logger is enabled.  For example, commandline
// flags might be used to set the logging verbosity and disable some info
// logs.
func (l *Logger) Enabled() bool {
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	return l.verbosity <= Verbosity(logLevel)
}

func sourcePath(file string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, file); err == nil && !strings.HasPrefix(rel, "../") {
			return rel
		}
	}
	return filepath.Join(filepath.Base(filepath.Dir(file)), filepath.Base(file))
}

// log will log the message. It DOES NOT check Enabled() first so that should
// be checked by it's callers
func (l *Logger) log(msg string, context map[string]interface{}) {
	_, file, line, _ := runtime.Caller(3)
	file = sourcePath(file)
	m := Line{
		Timestamp: TimestampFunc(),
		FileLine:  fmt.Sprintf("%s:%s", file, strconv.Itoa(line)),
		Verbosity: l.verbosity.String(),
		Component: l.name,
		Message:   msg,
		Context:   context,
	}

	err := l.encoder.Encode(l.output, m)
	if err != nil {
		// expand first so we can quote later
		orig := fmt.Sprintf("%#v", m)
		_, _ = fmt.Fprintf(l.output, `{"message","failed to encode message", "encoder":"%T","log":%q,"cause":%q}`, l.encoder, orig, err)
	}
}

// Info logs a non-error message with the given key/value pairs as context.
//
// The msg argument should be used to add some constant description to
// the log line.  The key/value pairs can then be used to add additional
// variable information.  The key/value pairs should alternate string
// keys and arbitrary values.
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	if !l.Enabled() {
		return
	}
	l.log(msg, combine(l.context, keysAndValues...))
}

// Error logs an error, with the given message and key/value pairs as context.
// It functions similarly to calling Info with the "error" named value, but may
// have unique behavior, and should be preferred for logging errors (see the
// package documentations for more information).
//
// The msg field should be used to add context to any underlying error,
// while the err field should be used to attach the actual error that
// triggered this log line, if present.
func (l *Logger) Error(err error, msg string, keysAndValues ...interface{}) {
	if !l.Enabled() {
		return
	}

	if err == nil {
		l.Info(msg, keysAndValues)
		return
	}

	switch err.(type) {
	case *kverrors.KVError:
		// nothing to be done
	default:
		err = kverrors.New(err.Error())
	}

	l.Info(msg, append(keysAndValues, ErrorKey, err)...)
}

// V returns an Logger value for a specific verbosity level, relative to
// this Logger.  In other words, V values are additive.  V higher verbosity
// level means a log message is less important.  It's illegal to pass a log
// level less than zero.
func (l *Logger) V(v int) logr.Logger {
	return NewLogger(l.name, l.output, Verbosity(v)+l.verbosity, l.encoder, l.context)
}

// WithName adds a new element to the logger's name.
// Successive calls with WithName continue to append
// suffixes to the logger's name.  It's strongly recommended
// that name segments contain only letters, digits, and hyphens
// (see the package documentation for more information).
func (l *Logger) WithName(name string) logr.Logger {
	newName := name

	if l.name != "" {
		newName = fmt.Sprintf("%s_%s", l.name, name)
	}

	return NewLogger(
		newName,
		l.output,
		l.verbosity,
		l.encoder,
		l.context,
	)
}
