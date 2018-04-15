package logging

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/user"
	"github.com/weaveworks/promrus"
)

const (
	defaultDedupeInterval = time.Minute
)

// Setup configures logging output to stderr, sets the log level and sets the formatter.
func Setup(logLevel string) error {
	log.SetOutput(os.Stderr)
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("Error parsing log level: %v", err)
	}
	log.SetLevel(level)
	log.SetFormatter(&textFormatter{})
	hook, err := promrus.NewPrometheusHook() // Expose number of log messages as Prometheus metrics.
	if err != nil {
		return err
	}
	log.AddHook(hook)
	return nil
}

// SetupDeduplication should be performed after any other logging setup.
// For all logs less severe or equal to the given log level (but still higher than the logger's configured log level),
// these logs will be 'deduplicated'. What this means is that, excluding certain special fields like time, multiple
// identical log entries will be grouped up and a summary message emitted.
// For example, instead of:
//     00:00:00 INFO User 123 did xyz
//     00:00:10 INFO User 123 did xyz
//     00:00:25 INFO User 123 did xyz
//     00:00:55 INFO User 123 did xyz
// you would get:
//     00:00:00 INFO User 123 did xyz
//     00:01:00 INFO Repeated 3 times: User 123 did xyz
// The interval argument controls how long to wait for additional messages to arrive before reporting.
// Increase it to deduplicate more aggressively, decrease it to lower latency from a log occurring to it appearing.
// Set it to 0 to pick a sensible default value (recommended).
// NOTE: For simplicity and efficiency, fields are considered 'equal' if and only if their string representations (%v) are equal.
func SetupDeduplication(logLevel string, interval time.Duration) error {
	dedupeLevel, err := log.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("Error parsing log level: %v", err)
	}
	if interval <= 0 {
		interval = defaultDedupeInterval
	}

	// We use a special Formatter to either format the log using the original formatter, or to return ""
	// so nothing will be written for that event. The repeated entries are later logged along with a field flag
	// that tells the formatter to ignore the message.
	stdLogger := log.StandardLogger()
	stdLogger.Formatter = newDedupeFormatter(stdLogger.Formatter, dedupeLevel, interval)
	return nil
}

type textFormatter struct{}

// Based off logrus.TextFormatter, which behaves completely
// differently when you don't want colored output
func (f *textFormatter) Format(entry *log.Entry) ([]byte, error) {
	b := &bytes.Buffer{}

	levelText := strings.ToUpper(entry.Level.String())[0:4]
	timeStamp := entry.Time.Format("2006/01/02 15:04:05.000000")
	fmt.Fprintf(b, "%s: %s %s", levelText, timeStamp, entry.Message)
	if len(entry.Data) > 0 {
		b.WriteString(" " + fieldsToString(entry.Data))
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// With returns a log entry with common Weaveworks logging information.
//
// e.g.
//     logger := logging.With(ctx)
//     logger.Errorf("Some error")
func With(ctx context.Context) *log.Entry {
	return log.WithFields(user.LogFields(ctx))
}

type entryCount struct {
	entry log.Entry
	count int
}

type dedupeFormatter struct {
	innerFormatter log.Formatter
	level          log.Level
	interval       time.Duration
	seen           map[string]entryCount
	lock           sync.Mutex
}

func newDedupeFormatter(innerFormatter log.Formatter, level log.Level, interval time.Duration) *dedupeFormatter {
	return &dedupeFormatter{
		innerFormatter: innerFormatter,
		level:          level,
		interval:       interval,
		seen:           map[string]entryCount{},
	}
}

func (f *dedupeFormatter) Format(entry *log.Entry) ([]byte, error) {
	if f.shouldLog(entry) {
		b, err := f.innerFormatter.Format(entry)
		return b, err
	}
	return []byte{}, nil
}

func (f *dedupeFormatter) shouldLog(entry *log.Entry) bool {
	if _, ok := entry.Data["deduplicated"]; ok {
		// ignore our own logs about deduped messages
		return true
	}
	if entry.Level < f.level {
		// ignore logs more severe than our level
		return true
	}
	key := fmt.Sprintf("%s %s", entry.Message, fieldsToString(entry.Data))
	f.lock.Lock()
	defer f.lock.Unlock()
	if ec, ok := f.seen[key]; ok {
		// already seen, increment count and do not log
		ec.count++
		f.seen[key] = ec
		return false
	}
	// New message, log it but add it to seen.
	// We need to copy because the pointer ceases to be valid after we return from Format
	f.seen[key] = entryCount{entry: *entry}
	go f.evictEntry(key) // queue to evict later
	return true
}

// Wait for interval seconds then evict the entry and send the log
func (f *dedupeFormatter) evictEntry(key string) {
	time.Sleep(f.interval)
	var ec entryCount
	func() {
		f.lock.Lock()
		defer f.lock.Unlock()
		ec = f.seen[key]
		delete(f.seen, key)
	}()
	if ec.count == 0 {
		return
	}
	entry := log.WithFields(ec.entry.Data).WithField("deduplicated", ec.count)
	message := fmt.Sprintf("Repeated %d times: %s", ec.count, ec.entry.Message)
	// There's no way to choose the log level dynamically, so we have to do this hack
	map[log.Level]func(args ...interface{}){
		log.PanicLevel: entry.Panic,
		log.FatalLevel: entry.Fatal,
		log.ErrorLevel: entry.Error,
		log.WarnLevel:  entry.Warn,
		log.InfoLevel:  entry.Info,
		log.DebugLevel: entry.Debug,
	}[ec.entry.Level](message)
}

func fieldsToString(data log.Fields) string {
	parts := make([]string, 0, len(data))
	// traversal order here is arbitrary but stable, which is fine for our purposes
	for k, v := range data {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}
