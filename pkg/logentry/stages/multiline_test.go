package stages

import (
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	ww "github.com/weaveworks/common/server"
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

	out := processEntries(stage, simpleEntry("START line 1"), simpleEntry("not a start line"), simpleEntry("START line 2"), simpleEntry("START line 3"))

	require.Len(t, out, 3)
	require.Equal(t, "START line 1\nnot a start line", out[0].Line)
	require.Equal(t, "START line 2", out[1].Line)
	require.Equal(t, "START line 3", out[2].Line)
}
func Test_multilineStage_MaxWaitTime(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util.InitLogger(cfg)
	Debug = true

	maxWait := time.Duration(2 * time.Second)
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
		return
	}()

	// Write input with a delay
	go func() {
		in <- simpleEntry("START line")

		// Trigger flush due to max wait timeout
		time.Sleep(2 * maxWait)

		in <- simpleEntry("not a start line hitting timeout")

		// Signal pipeline we are done.
		close(in)
		return
	}()

	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(res) == 2 }, time.Duration(3*maxWait), time.Second)
	require.Equal(t, "START line", res[0].Line)
	require.Equal(t, "not a start line hitting timeout", res[1].Line)
}

func simpleEntry(line string) Entry {
	return Entry{
		Extracted: map[string]interface{}{},
		Entry: api.Entry{
			Labels: model.LabelSet{},
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		},
	}

}
