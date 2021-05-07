package metric

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestHistogramExpiration(t *testing.T) {
	t.Parallel()
	cfg := HistogramConfig{}

	hist, err := NewHistograms("test1", "HELP ME!!!!!", cfg, 1)
	assert.Nil(t, err)

	// Create a label and increment the histogram
	lbl1 := model.LabelSet{}
	lbl1["test"] = "app"
	hist.With(lbl1).Observe(23)

	// Collect the metrics, should still find the metric in the map
	collect(hist)
	assert.Contains(t, hist.metrics, lbl1.Fingerprint())

	time.Sleep(1100 * time.Millisecond) // Wait just past our max idle of 1 sec

	//Add another histogram with new label val
	lbl2 := model.LabelSet{}
	lbl2["test"] = "app2"
	hist.With(lbl2).Observe(2)

	// Collect the metrics, first histogram should have expired and removed, second should still be present
	collect(hist)
	assert.NotContains(t, hist.metrics, lbl1.Fingerprint())
	assert.Contains(t, hist.metrics, lbl2.Fingerprint())
}
