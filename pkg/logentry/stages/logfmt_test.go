package stages

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

type testLogfmtData struct {
	entry        string
	entryCleaned string
	extracted    map[string]interface{}
	time         time.Time
}

var testLogfmtYaml = `
pipeline_stages:
- logfmt:
`

var testStaticTime = time.Now().Add(-time.Hour)

var testLogfmtLog = []testLogfmtData{
	{
		entry:        `ts=2019-06-14T12:45:43+00:00, lvl=debug, app=app, int=1234, float=1.337 msg="test log message"`,
		entryCleaned: `ts=2019-06-14T12:45:43+00:00 lvl=debug app=app int=1234 float=1.337 msg="test log message"`,
		extracted: map[string]interface{}{
			"ts":    "2019-06-14T12:45:43+00:00",
			"lvl":   "debug",
			"app":   "app",
			"int":   1234,
			"float": 1.337,
			"msg":   "test log message",
		},
		time: time.Date(2019, 06, 14, 12, 45, 43, 0, time.FixedZone("UTC", 0)),
	},
	{
		entry: `ts=2019-06-14T12:45:43+00:00 lvl=debug app=app int=1234 float=.337 msg="test log message"`,
		extracted: map[string]interface{}{
			"ts":    "2019-06-14T12:45:43+00:00",
			"lvl":   "debug",
			"app":   "app",
			"int":   1234,
			"float": .337,
			"msg":   "test log message",
		},
		time: time.Date(2019, 06, 14, 12, 45, 43, 0, time.FixedZone("UTC", 0)),
	},
	{
		entry: `ts=2019-06-14T12:45:43+00:00 testa="this is the first \"test\"" testb="this is the second test"`,
		extracted: map[string]interface{}{
			"ts":    "2019-06-14T12:45:43+00:00",
			"testa": `this is the first "test"`,
			"testb": "this is the second test",
		},
		time: time.Date(2019, 06, 14, 12, 45, 43, 0, time.FixedZone("UTC", 0)),
	},
	{
		entry:     `ts=`,
		extracted: map[string]interface{}{},
		time:      testStaticTime,
	},
}

func TestLogfmtPipeline(t *testing.T) {
	pl, err := NewPipeline(util.Logger, loadConfig(testLogfmtYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range testLogfmtLog {
		lbls := model.LabelSet{}
		ts := time.Now()
		extracted := map[string]interface{}{}
		entry := test.entry
		pl.Process(lbls, extracted, &ts, &entry)

		if test.entryCleaned == "" {
			test.entryCleaned = test.entry
		}
		assert.Equal(t, test.extracted, extracted, "invalid labels")
		assert.Equal(t, test.entryCleaned, entry, "invalid reformat of logline")
		if test.time.Unix() != testStaticTime.Unix() {
			assert.Equal(t, test.time.Unix(), ts.Unix(), "invalid time")
		}
	}

}
