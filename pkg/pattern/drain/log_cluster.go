package drain

import (
	"strings"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
)

type LogCluster struct {
	id         int
	Size       int
	Tokens     []string
	TokenState interface{}
	Stringer   func([]string, interface{}) string

	Chunks Chunks
}

func (c *LogCluster) String() string {
	if c.Stringer != nil {
		return c.Stringer(c.Tokens, c.TokenState)
	}
	return strings.Join(c.Tokens, " ")
}

func (c *LogCluster) append(ts model.Time) {
	c.Size++
	c.Chunks.Add(ts)
}

func (c *LogCluster) merge(samples []*logproto.PatternSample) {
	c.Size += int(sumSize(samples))
	c.Chunks.merge(samples)
}

func (c *LogCluster) Iterator(from, through, step model.Time) iter.Iterator {
	return c.Chunks.Iterator(c.String(), from, through, step)
}

func (c *LogCluster) Samples() []*logproto.PatternSample {
	return c.Chunks.samples()
}

func (c *LogCluster) Prune(olderThan time.Duration) {
	c.Chunks.prune(olderThan)
	c.Size = c.Chunks.size()
}

func sumSize(samples []*logproto.PatternSample) int64 {
	var x int64
	for i := range samples {
		x += samples[i].Value
	}
	return x
}
