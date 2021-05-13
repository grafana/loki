package stages

import (
	"testing"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	json "github.com/json-iterator/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDockerRecombine(t *testing.T) {
	m, err := newDockerRecombine(util_log.Logger)
	require.NoError(t, err)

	h := func(s, stream string, t time.Time) Entry {
		buf, err := json.Marshal(struct {
			Output    string    `json:"output"`
			Stream    string    `json:"stream"`
			Timestamp time.Time `json:"timestamp"`
		}{s, stream, t})
		if err != nil {
			panic(err)
		}
		return newEntry(map[string]interface{}{
			"output":    s,
			"stream":    stream,
			"timestamp": t,
		}, nil, string(buf), t)
	}
	t1 := time.Now()
	t2 := t1.Add(1 * time.Minute)
	t3 := t2.Add(1 * time.Minute)
	in := []Entry{
		h("abc\n", "stdout", t1),

		h("no end of line here, ", "stdout", t1),
		h("but here it is\n", "stdout", t2),

		h("stdout start, ", "stdout", t1),
		h("stdout middle, ", "stdout", t2),
		h("stderr start, ", "stderr", t1),
		h("stdout end\n", "stdout", t3),

		h("stderr middle, ", "stderr", t2),
		h("stderr end\n", "stderr", t3),
	}
	expectedOut := []Entry{
		h("abc\n", "stdout", t1),
		h("no end of line here, but here it is\n", "stdout", t2),
		h("stdout start, stdout middle, stdout end\n", "stdout", t3),
		h("stderr start, stderr middle, stderr end\n", "stderr", t3),
	}
	out := processEntries(m, in...)
	require.Len(t, out, len(expectedOut))
	for i, o := range out {
		eo := expectedOut[i]
		assert.Equal(t, eo.Extracted, o.Extracted)
		assert.Equal(t, eo.Timestamp, o.Timestamp)
		assert.Equal(t, eo.Labels, o.Labels)
	}
}
