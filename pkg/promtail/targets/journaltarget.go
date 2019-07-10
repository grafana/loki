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
	relabelConfig []*relabel.Config,
	targetConfig *scrape.JournalTargetConfig,
) (*JournalTarget, error) {

	return journalTargetWithReader(
		logger,
		handler,
		positions,
		relabelConfig,
		targetConfig,
		defaultJournalReaderFunc,
	)
}

func journalTargetWithReader(
	logger log.Logger,
	handler api.EntryHandler,
	positions *positions.Positions,
	relabelConfig []*relabel.Config,
	targetConfig *scrape.JournalTargetConfig,
	readerFunc journalReaderFunc,
) (*JournalTarget, error) {

	if readerFunc == nil {
		readerFunc = defaultJournalReaderFunc
	}

	until := make(chan time.Time)
	t := &JournalTarget{
		logger:        logger,
		handler:       handler,
		positions:     positions,
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
		Since:     targetConfig.Since,
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

	// TODO(rfratto): positions support? There are two ways to be able to
	// uniquely identify the offset from a journal entry: monotonic timestamps
	// and the cursor position.
	//
	// The monotonic timestamp is a tuple of a 128-bit boot ID and a 64-bit
	// nanosecond timestamp. The sdjournal library currently does not expose
	// the seek functionality for monotonic timestamps.
	//
	// The cursor position is an arbtirary string identifying the offset
	// in a journal. The documentation for systemd declares the cursor
	// string as opaque and shouldn't be parsed by users. The sdjournal
	// library *does* expose the seek funcationlity using the cursor
	// position.
	//
	// With either solution, the current positions.Positions is unable
	// to handle the timestamps as it is only equipped to handle int64 ts.

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

// TODO(rfratto): Perhaps the Target interface should remove DiscoveredLabels
// and Labels and instead have a Dropped method.

// DiscoveredLabels satisfies the Target interface. Returns nil for
// JournalTarget as there are no discovered labels present as a
// JournalTarget processes.
func (t *JournalTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels satisfies the Target interface. Returns nil for JournalTarget
// as there are is no guaranteed constant list of labels present as a
// JournalTarget processes.
func (t *JournalTarget) Labels() model.LabelSet {
	return nil
}

// Details returns target-specific details (currently nil).
func (t *JournalTarget) Details() interface{} {
	return nil
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
