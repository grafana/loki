package journal

import (
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/coreos/go-systemd/sdjournal"
	"github.com/grafana/logish/pkg/promtail"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/relabel"
	log "github.com/sirupsen/logrus"
)

type JournalTarget struct {
	client *promtail.Client

	journal *sdjournal.JournalReader
	until   chan time.Time

	relabelConfigs []*config.RelabelConfig
}

func NewJournalTarget(c *promtail.Client, positions *promtail.Positions, path string, job string, relabelConfigs []*config.RelabelConfig) (*JournalTarget, error) {
	log.Infof("new journal target: %s", job)

	since := time.Duration(0)
	posPath := fmt.Sprintf("journal-%s", job)
	position := positions.Get(posPath)

	if position != int64(0) {
		lastTime := time.Unix(0, position)
		since = lastTime.Sub(time.Now())
	}

	journalEntryFormatter := func(e *sdjournal.JournalEntry) (string, error) {
		line, ok := e.Fields["MESSAGE"]
		if !ok {
			log.Debug("empty message", e.Fields)
			return "", nil
		}

		labels := labelsFromFields(e.Fields)
		labels = relabel.Process(labels, relabelConfigs...)
		for k := range labels {
			if k[0:2] == "__" {
				delete(labels, k)
			}
		}

		// Drop empty targets.
		if labels == nil {
			return line, nil
		}

		usec := e.RealtimeTimestamp
		ts := int64(usec) * int64(time.Microsecond)
		positions.Put(posPath, ts)
		return line, c.Line(labels, time.Unix(0, ts), line)
	}

	journal, err := sdjournal.NewJournalReader(sdjournal.JournalReaderConfig{
		Since:     since,
		Path:      path,
		Formatter: journalEntryFormatter,
	})
	if err != nil {
		return nil, errors.Wrap(err, "creating journal reader")
	}

	until := make(chan time.Time)
	journal.Follow(until, ioutil.Discard)

	return &JournalTarget{
		client: c,

		journal: journal,
		until:   until,

		relabelConfigs: relabelConfigs,
	}, nil
}

func (jt *JournalTarget) Stop() error {
	jt.until <- time.Now()
	return nil
}

func labelsFromFields(fields map[string]string) model.LabelSet {
	l := model.LabelSet{}
	for k, v := range fields {
		l[model.LabelName(fmt.Sprintf("__journal_%s", strings.ToLower(k)))] = model.LabelValue(v)
	}

	return l
}
