package stages

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	ww "github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
)

func Test_multilineStage_Process(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util.InitLogger(cfg)
	Debug = true

	mcfg := &MultilineConfig{Expression: ptrFromString("^START"), MaxWaitTime: ptrFromString("3s")}
	err := validateMultilineConfig(mcfg)
	require.NoError(t, err)

	stage := &multilineStage{
		cfg:    mcfg,
		logger: util.Logger,
	}

	out := processEntries(stage,
		simpleEntry("not a start line before 1", "label"),
		simpleEntry("not a start line before 2", "label"),
		simpleEntry("START line 1", "label"),
		simpleEntry("not a start line", "label"),
		simpleEntry("START line 2", "label"),
		simpleEntry("START line 3", "label"))

	require.Len(t, out, 5)
	require.Equal(t, "not a start line before 1", out[0].Line)
	require.Equal(t, "not a start line before 2", out[1].Line)
	require.Equal(t, "START line 1\nnot a start line", out[2].Line)
	require.Equal(t, "START line 2", out[3].Line)
	require.Equal(t, "START line 3", out[4].Line)
}

func Test_multilineStage_MultiStreams(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util.InitLogger(cfg)
	Debug = true

	mcfg := &MultilineConfig{Expression: ptrFromString("^START"), MaxWaitTime: ptrFromString("3s")}
	err := validateMultilineConfig(mcfg)
	require.NoError(t, err)

	stage := &multilineStage{
		cfg:    mcfg,
		logger: util.Logger,
	}

	out := processEntries(stage,
		simpleEntry("START line 1", "one"),
		simpleEntry("not a start line 1", "one"),
		simpleEntry("START line 1", "two"),
		simpleEntry("not a start line 2", "one"),
		simpleEntry("START line 2", "two"),
		simpleEntry("START line 2", "one"),
		simpleEntry("not a start line 1", "one"),
	)

	sort.Slice(out, func(l, r int) bool {
		return out[l].Timestamp.Before(out[r].Timestamp)
	})

	require.Len(t, out, 4)

	require.Equal(t, "START line 1\nnot a start line 1\nnot a start line 2", out[0].Line)
	require.Equal(t, model.LabelValue("one"), out[0].Labels["value"])

	require.Equal(t, "START line 1", out[1].Line)
	require.Equal(t, model.LabelValue("two"), out[1].Labels["value"])

	require.Equal(t, "START line 2", out[2].Line)
	require.Equal(t, model.LabelValue("two"), out[2].Labels["value"])

	require.Equal(t, "START line 2\nnot a start line 1", out[3].Line)
	require.Equal(t, model.LabelValue("one"), out[3].Labels["value"])
}

func Test_multilineStage_MaxWaitTime(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util.InitLogger(cfg)
	Debug = true

	maxWait := 2 * time.Second
	mcfg := &MultilineConfig{Expression: ptrFromString("^START"), MaxWaitTime: ptrFromString(maxWait.String())}
	err := validateMultilineConfig(mcfg)
	require.NoError(t, err)

	stage := &multilineStage{
		cfg:    mcfg,
		logger: util.Logger,
	}

	in := make(chan Entry, 2)
	out := stage.Run(in)

	// Accumulate result
	mu := new(sync.Mutex)
	var res []Entry
	go func() {
		for e := range out {
			mu.Lock()
			t.Logf("appending %s", e.Line)
			res = append(res, e)
			mu.Unlock()
		}
	}()

	// Write input with a delay
	go func() {
		in <- simpleEntry("START line", "label")

		// Trigger flush due to max wait timeout
		time.Sleep(2 * maxWait)

		in <- simpleEntry("not a start line hitting timeout", "label")

		// Signal pipeline we are done.
		close(in)
	}()

	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(res) == 2 }, 3*maxWait, time.Second)
	require.Equal(t, "START line", res[0].Line)
	require.Equal(t, "not a start line hitting timeout", res[1].Line)
}

func simpleEntry(line, label string) Entry {
	return Entry{
		Extracted: map[string]interface{}{},
		Entry: api.Entry{
			Labels: model.LabelSet{"value": model.LabelValue(label)},
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		},
	}

}
