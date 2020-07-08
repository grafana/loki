// +build linux,cgo

package journal

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/targets/target"

	"github.com/go-kit/kit/log"

	"github.com/grafana/loki/pkg/promtail/scrapeconfig"

	"github.com/coreos/go-systemd/sdjournal"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
)

const (
	// journalEmptyStr is represented as a single-character space because
	// returning an empty string from sdjournal.JournalReaderConfig's
	// Formatter causes an immediate EOF and induces performance issues
	// with how that is handled in sdjournal.
	journalEmptyStr = " "

	// journalDefaultMaxAgeTime represents the default earliest entry that
	// will be read by the journal reader if there is no saved position
	// newer than the "max_age" time.
	journalDefaultMaxAgeTime = time.Hour * 7
)

type journalReader interface {
	io.Closer
	Follow(until <-chan time.Time, writer io.Writer) error
}

// Abstracted functions for interacting with the journal, used for mocking in tests:
type journalReaderFunc func(sdjournal.JournalReaderConfig) (journalReader, error)
type journalEntryFunc func(cfg sdjournal.JournalReaderConfig, cursor string) (*sdjournal.JournalEntry, error)

// Default implementations of abstracted functions:
var defaultJournalReaderFunc = func(c sdjournal.JournalReaderConfig) (journalReader, error) {
	return sdjournal.NewJournalReader(c)
}

var defaultJournalEntryFunc = func(c sdjournal.JournalReaderConfig, cursor string) (*sdjournal.JournalEntry, error) {
	var (
		journal *sdjournal.Journal
		err     error
	)

	if c.Path != "" {
		journal, err = sdjournal.NewJournalFromDir(c.Path)
	} else {
		journal, err = sdjournal.NewJournal()
	}

	if err != nil {
		return nil, err
	} else if err := journal.SeekCursor(cursor); err != nil {
		return nil, err
	}

	// Just seeking the cursor won't give us the entry. We should call Next() or Previous()
	// to get the closest following or the closest preceding entry. We have chosen here to call Next(),
	// reason being, if we call Previous() we would re read an already read entry.
	// More info here https://www.freedesktop.org/software/systemd/man/sd_journal_seek_cursor.html#
	_, err = journal.Next()
	if err != nil {
		return nil, err
	}

	return journal.GetEntry()
}

// JournalTarget tails systemd journal entries.
type JournalTarget struct {
	logger        log.Logger
	handler       api.EntryHandler
	positions     positions.Positions
	positionPath  string
	relabelConfig []*relabel.Config
	config        *scrapeconfig.JournalTargetConfig
	labels        model.LabelSet

	r     journalReader
	until chan time.Time
}

// NewJournalTarget configures a new JournalTarget.
func NewJournalTarget(
	logger log.Logger,
	handler api.EntryHandler,
	positions positions.Positions,
	jobName string,
	relabelConfig []*relabel.Config,
	targetConfig *scrapeconfig.JournalTargetConfig,
) (*JournalTarget, error) {

	return journalTargetWithReader(
		logger,
		handler,
		positions,
		jobName,
		relabelConfig,
		targetConfig,
		defaultJournalReaderFunc,
		defaultJournalEntryFunc,
	)
}

func journalTargetWithReader(
	logger log.Logger,
	handler api.EntryHandler,
	positions positions.Positions,
	jobName string,
	relabelConfig []*relabel.Config,
	targetConfig *scrapeconfig.JournalTargetConfig,
	readerFunc journalReaderFunc,
	entryFunc journalEntryFunc,
) (*JournalTarget, error) {

	positionPath := fmt.Sprintf("journal-%s", jobName)
	position := positions.GetString(positionPath)

	if readerFunc == nil {
		readerFunc = defaultJournalReaderFunc
	}
	if entryFunc == nil {
		entryFunc = defaultJournalEntryFunc
	}

	until := make(chan time.Time)
	t := &JournalTarget{
		logger:        logger,
		handler:       handler,
		positions:     positions,
		positionPath:  positionPath,
		relabelConfig: relabelConfig,
		labels:        targetConfig.Labels,
		config:        targetConfig,

		until: until,
	}

	var maxAge time.Duration
	var err error
	if targetConfig.MaxAge == "" {
		maxAge = journalDefaultMaxAgeTime
	} else {
		maxAge, err = time.ParseDuration(targetConfig.MaxAge)
	}
	if err != nil {
		return nil, errors.Wrap(err, "parsing journal reader 'max_age' config value")
	}

	cfg := t.generateJournalConfig(journalConfigBuilder{
		JournalPath: targetConfig.Path,
		Position:    position,
		MaxAge:      maxAge,
		EntryFunc:   entryFunc,
	})
	t.r, err = readerFunc(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "creating journal reader")
	}

	go func() {
		err := t.r.Follow(until, ioutil.Discard)
		if err != nil && err != sdjournal.ErrExpired {
			level.Error(t.logger).Log("msg", "received error during sdjournal follow", "err", err.Error())
		}
	}()

	return t, nil
}

type journalConfigBuilder struct {
	JournalPath string
	Position    string
	MaxAge      time.Duration
	EntryFunc   journalEntryFunc
}

// generateJournalConfig generates a journal config by trying to intelligently
// determine if a time offset or the cursor should be used for the starting
// position in the reader.
func (t *JournalTarget) generateJournalConfig(
	cb journalConfigBuilder,
) sdjournal.JournalReaderConfig {

	cfg := sdjournal.JournalReaderConfig{
		Path:      cb.JournalPath,
		Formatter: t.formatter,
	}

	// When generating the JournalReaderConfig, we want to preferably
	// use the Cursor, since it's guaranteed unique to a given journal
	// entry. When we don't know the cursor position (or want to set
	// a start time), we'll fall back to the less-precise Since, which
	// takes a negative duration back from the current system time.
	//
	// The presence of Since takes precedence over Cursor, so we only
	// ever set one and not both here.

	if cb.Position == "" {
		cfg.Since = -1 * cb.MaxAge
		return cfg
	}

	// We have a saved position and need to get that entry to see if it's
	// older than cb.MaxAge. If it _is_ older, then we need to use cfg.Since
	// rather than cfg.Cursor.
	entry, err := cb.EntryFunc(cfg, cb.Position)
	if err != nil {
		level.Error(t.logger).Log("msg", "received error reading saved journal position", "err", err.Error())
		cfg.Since = -1 * cb.MaxAge
		return cfg
	}

	ts := time.Unix(0, int64(entry.RealtimeTimestamp)*int64(time.Microsecond))
	if time.Since(ts) > cb.MaxAge {
		cfg.Since = -1 * cb.MaxAge
		return cfg
	}

	cfg.Cursor = cb.Position
	return cfg
}

func (t *JournalTarget) formatter(entry *sdjournal.JournalEntry) (string, error) {
	ts := time.Unix(0, int64(entry.RealtimeTimestamp)*int64(time.Microsecond))

	var msg string

	if t.config.JSON {
		json := jsoniter.ConfigCompatibleWithStandardLibrary

		bb, err := json.Marshal(entry.Fields)
		if err != nil {
			level.Error(t.logger).Log("msg", "could not marshal journal fields to JSON", "err", err)
			return journalEmptyStr, nil
		}
		msg = string(bb)
	} else {
		var ok bool
		msg, ok = entry.Fields["MESSAGE"]
		if !ok {
			level.Debug(t.logger).Log("msg", "received journal entry with no MESSAGE field")
			return journalEmptyStr, nil
		}
	}

	entryLabels := makeJournalFields(entry.Fields)

	// Add constant labels
	for k, v := range t.labels {
		entryLabels[string(k)] = string(v)
	}

	processedLabels := relabel.Process(labels.FromMap(entryLabels), t.relabelConfig...)

	processedLabelsMap := processedLabels.Map()
	labels := make(model.LabelSet, len(processedLabelsMap))
	for k, v := range processedLabelsMap {
		if k[0:2] == "__" {
			continue
		}

		labels[model.LabelName(k)] = model.LabelValue(v)
	}
	if len(labels) == 0 {
		// No labels, drop journal entry
		return journalEmptyStr, nil
	}

	t.positions.PutString(t.positionPath, entry.Cursor)
	err := t.handler.Handle(labels, ts, msg)
	return journalEmptyStr, err
}

// Type returns JournalTargetType.
func (t *JournalTarget) Type() target.TargetType {
	return target.JournalTargetType
}

// Ready indicates whether or not the journal is ready to be
// read from.
func (t *JournalTarget) Ready() bool {
	return true
}

// DiscoveredLabels returns the set of labels discovered by
// the JournalTarget, which is always nil. Implements
// Target.
func (t *JournalTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to
// all log entries produced by the JournalTarget.
func (t *JournalTarget) Labels() model.LabelSet {
	return t.labels
}

// Details returns target-specific details.
func (t *JournalTarget) Details() interface{} {
	return map[string]string{
		"position": t.positions.GetString(t.positionPath),
	}
}

// Stop shuts down the JournalTarget.
func (t *JournalTarget) Stop() error {
	t.until <- time.Now()
	return t.r.Close()
}

func makeJournalFields(fields map[string]string) map[string]string {
	result := make(map[string]string, len(fields))
	for k, v := range fields {
		if k == "PRIORITY" {
			result[fmt.Sprintf("__journal_%s_%s", strings.ToLower(k), "keyword")] = makeJournalPriority(v)
		}
		result[fmt.Sprintf("__journal_%s", strings.ToLower(k))] = v
	}
	return result
}

func makeJournalPriority(priority string) string {
	switch priority {
	case "0":
		return "emerg"
	case "1":
		return "alert"
	case "2":
		return "crit"
	case "3":
		return "error"
	case "4":
		return "warning"
	case "5":
		return "notice"
	case "6":
		return "info"
	case "7":
		return "debug"
	}
	return priority
}
