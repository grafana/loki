// +build linux

package targets

import (
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/promtail/scrape"

	"github.com/coreos/go-systemd/journal"
	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/stretchr/testify/require"
)

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

	jt, err := NewJournalTarget(logger, client, ps, relabels, &scrape.JournalTargetConfig{
		Since: time.Duration(1),
	})
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		journal.Send("ping", journal.PriInfo, map[string]string{
			"CODE_FILE": "journaltarget_test.go",
		})
	}

	// Give time for messages to be processed
	time.Sleep(time.Millisecond * 500)
	assert.Len(t, client.messages, 10)
	require.NoError(t, jt.Stop())
}
