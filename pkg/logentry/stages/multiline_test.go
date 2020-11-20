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

	mcfg := &MultilineConfig{Expression: ptrFromString("^START"),}
	validateMultilineConfig(mcfg)
	stage := &multilineStage{
		cfg: mcfg,
		logger: util.Logger,
		buffer: new(bytes.Buffer),
	}

	stage.Process(model.LabelSet{}, map[string]interface{}{}, ptrFromTime(time.Now()), ptrFromString("START line 1"))
	stage.Process(model.LabelSet{}, map[string]interface{}{}, ptrFromTime(time.Now()), ptrFromString("not a start line"))

	nextStart := "START line 2"
	stage.Process(model.LabelSet{}, map[string]interface{}{}, ptrFromTime(time.Now()), &nextStart)

	require.Equal(t, "START line 1\nnot a start line", nextStart)

	nextStart = "START line 3"
	stage.Process(model.LabelSet{}, map[string]interface{}{}, ptrFromTime(time.Now()), &nextStart)

	require.Equal(t, "START line 2", nextStart)
}