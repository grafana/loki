package stages

import (
	"bytes"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
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

	mcfg := &MultilineConfig{Expression: ptrFromString("^START")}
	err := validateMultilineConfig(mcfg)
	require.NoError(t, err)

	stage := &multilineStage{
		cfg:    mcfg,
		logger: util.Logger,
		buffer: new(bytes.Buffer),
	}

	out := processEntries(stage, simpleEntry("START line 1"), simpleEntry("not a start line"), simpleEntry("START line 2"), simpleEntry("START line 3"))

	require.Equal(t, "START line 1\nnot a start line", out[0].Line)
	require.Equal(t, "START line 2", out[1].Line)
	require.Equal(t, "START line 3", out[2].Line)
}

func simpleEntry(line string) Entry {
	return Entry{
				Labels:    model.LabelSet{},
				Line:      ptrFromString(line),
				Extracted: map[string]interface{}{},
				Timestamp: ptrFromTime(time.Now()),
			}

}