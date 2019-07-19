// +build linux,cgo

package targets

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/coreos/go-systemd/sdjournal"

	"gopkg.in/yaml.v2"

	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/promtail/scrape"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/stretchr/testify/require"
)

type mockJournalReader struct {
	config sdjournal.JournalReaderConfig
	t      *testing.T
}

func newMockJournalReader(c sdjournal.JournalReaderConfig) (journalReader, error) {
	return &mockJournalReader{config: c}, nil
}

func (r *mockJournalReader) Close() error {
	return nil
}

func (r *mockJournalReader) Follow(until <-chan time.Time, writer io.Writer) error {
	<-until
	return nil
}

func (r *mockJournalReader) Write(msg string, fields map[string]string) {
	allFields := make(map[string]string, len(fields))
	for k, v := range fields {
		allFields[k] = v
	}
	allFields["MESSAGE"] = msg

	ts := uint64(time.Now().UnixNano())

	_, err := r.config.Formatter(&sdjournal.JournalEntry{
		Fields:             allFields,
		MonotonicTimestamp: ts,
		RealtimeTimestamp:  ts,
	})
	assert.NoError(r.t, err)
}

func TestJournalTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFileName := dirName + "/positions.yml"

	// Set the sync period to a really long value, to guarantee the sync timer
	// never runs, this way we know everything saved was done through channel
	// notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	relabelCfg := `
- source_labels: ['__journal_code_file']
  regex: 'journaltarget_test\.go'
  action: 'keep'
- source_labels: ['__journal_code_file']
  target_label: 'code_file'`

	var relabels []*relabel.Config
	err = yaml.Unmarshal([]byte(relabelCfg), &relabels)
	require.NoError(t, err)

	jt, err := journalTargetWithReader(logger, client, ps, "test", relabels,
		&scrape.JournalTargetConfig{}, newMockJournalReader)
	require.NoError(t, err)

	r := jt.r.(*mockJournalReader)
	r.t = t

	for i := 0; i < 10; i++ {
		r.Write("ping", map[string]string{
			"CODE_FILE": "journaltarget_test.go",
		})
		assert.NoError(t, err)
	}

	assert.Len(t, client.messages, 10)
	require.NoError(t, jt.Stop())
}
