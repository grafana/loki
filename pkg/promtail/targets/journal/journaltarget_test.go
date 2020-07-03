// +build linux,cgo

package journal

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/coreos/go-systemd/sdjournal"

	"gopkg.in/yaml.v2"

	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/testutils"

	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/promtail/positions"
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

func newMockJournalEntry(entry *sdjournal.JournalEntry) journalEntryFunc {
	return func(c sdjournal.JournalReaderConfig, cursor string) (*sdjournal.JournalEntry, error) {
		return entry, nil
	}
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

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
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

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
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
		&scrapeconfig.JournalTargetConfig{}, newMockJournalReader, newMockJournalEntry(nil))
	require.NoError(t, err)

	r := jt.r.(*mockJournalReader)
	r.t = t

	for i := 0; i < 10; i++ {
		r.Write("ping", map[string]string{
			"CODE_FILE": "journaltarget_test.go",
		})
		assert.NoError(t, err)
	}
	fmt.Println(client.Messages)
	assert.Len(t, client.Messages, 10)
	require.NoError(t, jt.Stop())
}

func TestJournalTarget_JSON(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
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

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
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

	cfg := &scrapeconfig.JournalTargetConfig{JSON: true}

	jt, err := journalTargetWithReader(logger, client, ps, "test", relabels,
		cfg, newMockJournalReader, newMockJournalEntry(nil))
	require.NoError(t, err)

	r := jt.r.(*mockJournalReader)
	r.t = t

	for i := 0; i < 10; i++ {
		r.Write("ping", map[string]string{
			"CODE_FILE":   "journaltarget_test.go",
			"OTHER_FIELD": "foobar",
		})
		assert.NoError(t, err)

		expectMsg := `{"CODE_FILE":"journaltarget_test.go","MESSAGE":"ping","OTHER_FIELD":"foobar"}`

		require.Greater(t, len(client.Messages), 0)
		require.Equal(t, expectMsg, client.Messages[len(client.Messages)-1].Log)
	}

	assert.Len(t, client.Messages, 10)
	require.NoError(t, jt.Stop())
}

func TestJournalTarget_Since(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
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

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
	}

	cfg := scrapeconfig.JournalTargetConfig{
		MaxAge: "4h",
	}

	jt, err := journalTargetWithReader(logger, client, ps, "test", nil,
		&cfg, newMockJournalReader, newMockJournalEntry(nil))
	require.NoError(t, err)

	r := jt.r.(*mockJournalReader)
	require.Equal(t, r.config.Since, -1*time.Hour*4)
}

func TestJournalTarget_Cursor_TooOld(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
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
	ps.PutString("journal-test", "foobar")

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
	}

	cfg := scrapeconfig.JournalTargetConfig{}

	entryTs := time.Date(1980, time.July, 3, 12, 0, 0, 0, time.UTC)
	journalEntry := newMockJournalEntry(&sdjournal.JournalEntry{
		Cursor:            "foobar",
		Fields:            nil,
		RealtimeTimestamp: uint64(entryTs.UnixNano()),
	})

	jt, err := journalTargetWithReader(logger, client, ps, "test", nil,
		&cfg, newMockJournalReader, journalEntry)
	require.NoError(t, err)

	r := jt.r.(*mockJournalReader)
	require.Equal(t, r.config.Since, -1*time.Hour*7)
}

func TestJournalTarget_Cursor_NotTooOld(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
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
	ps.PutString("journal-test", "foobar")

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
	}

	cfg := scrapeconfig.JournalTargetConfig{}

	entryTs := time.Now().Add(-time.Hour)
	journalEntry := newMockJournalEntry(&sdjournal.JournalEntry{
		Cursor:            "foobar",
		Fields:            nil,
		RealtimeTimestamp: uint64(entryTs.UnixNano() / int64(time.Microsecond)),
	})

	jt, err := journalTargetWithReader(logger, client, ps, "test", nil,
		&cfg, newMockJournalReader, journalEntry)
	require.NoError(t, err)

	r := jt.r.(*mockJournalReader)
	require.Equal(t, r.config.Since, time.Duration(0))
	require.Equal(t, r.config.Cursor, "foobar")
}

func Test_MakeJournalFields(t *testing.T) {
	entryFields := map[string]string{
		"CODE_FILE":   "journaltarget_test.go",
		"OTHER_FIELD": "foobar",
		"PRIORITY":    "6",
	}
	receivedFields := makeJournalFields(entryFields)
	expectedFields := map[string]string{
		"__journal_code_file":        "journaltarget_test.go",
		"__journal_other_field":      "foobar",
		"__journal_priority":         "6",
		"__journal_priority_keyword": "info",
	}
	assert.Equal(t, expectedFields, receivedFields)
}
