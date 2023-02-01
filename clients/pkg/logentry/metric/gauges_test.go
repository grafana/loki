package metric

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestGaugeExpiration(t *testing.T) {
	t.Parallel()
	cfg := GaugeConfig{
		Action: "inc",
	}
	gcInterval = 100 * time.Millisecond

	gag, err := NewGauges("test1", "HELP ME!!!!!", cfg, 1, nil)
	assert.Nil(t, err)

	// Create a label and increment the gauge
	lbl1 := model.LabelSet{}
	lbl1["test"] = "app"
	with, err := gag.With(lbl1)
	if err != nil {
		assert.Nil(t, err)
	}
	with.Inc()

	// Collect the metrics, should still find the metric in the map
	//collect(gag)
	assert.Contains(t, gag.metrics, lbl1.Fingerprint())

	time.Sleep(1100 * time.Millisecond) // Wait just past our max idle of 1 sec

	//Add another gauge with new label val
	lbl2 := model.LabelSet{}
	lbl2["test"] = "app2"
	gauge, err := gag.With(lbl2)
	if err != nil {
		assert.Nil(t, err)
	}

	gauge.Inc()

	// Collect the metrics, first gauge should have expired and removed, second should still be present
	//collect(gag)
	assert.NotContains(t, gag.metrics, lbl1.Fingerprint())
	assert.Contains(t, gag.metrics, lbl2.Fingerprint())
}
