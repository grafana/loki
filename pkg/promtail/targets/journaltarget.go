// +build linux,cgo

package targets

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/promtail/positions"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/promtail/scrape"

	"github.com/coreos/go-systemd/sdjournal"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	// journalEmptyStr is represented as a single-character space because
	// returning an empty string from sdjournal.JournalReaderConfig's
	// Formatter causes an immediate EOF and induces performance issues
	// with how that is handled in sdjournal.
	journalEmptyStr = " "
)

type journalReader interface {
	io.Closer
	Follow(until <-chan time.Time, writer io.Writer) error
}

type journalReaderFunc func(sdjournal.JournalReaderConfig) (journalReader, error)

var defaultJournalReaderFunc = func(c sdjournal.JournalReaderConfig) (journalReader, error) {
	return sdjournal.NewJournalReader(c)
}

// JournalTarget tails systemd journal entries.
type JournalTarget struct {
	logger        log.Logger
	handler       api.EntryHandler
	positions     *positions.Positions
	positionPath  string
	relabelConfig []*relabel.Config
	config        *scrape.JournalTargetConfig
	labels        model.LabelSet

	r     journalReader
	until chan time.Time
}

// NewJournalTarget configures a new JournalTarget.
func NewJournalTarget(
	logger log.Logger,
	handler api.EntryHandler,
	positions *positions.Positions,
	jobName string,
	relabelConfig []*relabel.Config,
	targetConfig *scrape.JournalTargetConfig,
) (*JournalTarget, error) {

	return journalTargetWithReader(
		logger,
		handler,
		positions,
		jobName,
		relabelConfig,
		targetConfig,
		defaultJournalReaderFunc,
	)
}

func journalTargetWithReader(
	logger log.Logger,
	handler api.EntryHandler,
	positions *positions.Positions,
	jobName string,
	relabelConfig []*relabel.Config,
	targetConfig *scrape.JournalTargetConfig,
	readerFunc journalReaderFunc,
) (*JournalTarget, error) {

	positionPath := fmt.Sprintf("journal-%s", jobName)
	position := positions.GetString(positionPath)

	if readerFunc == nil {
		readerFunc = defaultJournalReaderFunc
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

	// Default to system path if not defined. Passing an empty string to
	// sdjournal is valid but forces reads from the journal to be from
	// the local machine id only, which contradicts the default behavior
	// of when a path is specified. To standardize, we manually default the
	// path here.
	journalPath := targetConfig.Path
	if journalPath == "" {
		journalPath = "/var/log/journal"
	}

	var err error
	t.r, err = readerFunc(sdjournal.JournalReaderConfig{
		Path:      journalPath,
		Cursor:    position,
		Formatter: t.formatter,
	})
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

func (t *JournalTarget) formatter(entry *sdjournal.JournalEntry) (string, error) {
	ts := time.Unix(0, int64(entry.RealtimeTimestamp)*int64(time.Microsecond))

	msg, ok := entry.Fields["MESSAGE"]
	if !ok {
		level.Debug(t.logger).Log("msg", "received journal entry with no MESSAGE field")
		return journalEmptyStr, nil
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
func (t *JournalTarget) Type() TargetType {
	return JournalTargetType
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
		result[fmt.Sprintf("__journal_%s", strings.ToLower(k))] = v
	}
	return result
}
