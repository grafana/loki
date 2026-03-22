package detected

import (
	"testing"

	"github.com/axiomhq/hyperloglog"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestUnmarshalDetectedLabel(t *testing.T) {
	t.Run("valid sketch", func(t *testing.T) {
		// Create a sketch with some data
		sketch := hyperloglog.New()
		sketch.Insert([]byte("value1"))
		sketch.Insert([]byte("value2"))
		sketchData, err := sketch.MarshalBinary()
		require.NoError(t, err)

		label := &logproto.DetectedLabel{
			Label:  "app",
			Sketch: sketchData,
		}

		result, err := unmarshalDetectedLabel(label)
		require.NoError(t, err)
		require.Equal(t, "app", result.Label)
		require.InDelta(t, sketch.Estimate(), result.Sketch.Estimate(), 0.001)
	})

	t.Run("invalid sketch data", func(t *testing.T) {
		label := &logproto.DetectedLabel{
			Label:  "app",
			Sketch: []byte("invalid sketch data"),
		}

		_, err := unmarshalDetectedLabel(label)
		require.Error(t, err)
	})
}

func TestUnmarshaledDetectedLabel_Merge(t *testing.T) {
	t.Run("merge valid sketches", func(t *testing.T) {
		// Create first sketch
		sketch1 := hyperloglog.New()
		for i := 0; i < 100; i++ {
			sketch1.Insert([]byte("value1-" + string(rune(i))))
		}
		sketch1Data, err := sketch1.MarshalBinary()
		require.NoError(t, err)

		// Create second sketch with some overlap
		sketch2 := hyperloglog.New()
		for i := 50; i < 150; i++ { // 50 overlapping values
			sketch2.Insert([]byte("value1-" + string(rune(i))))
		}
		sketch2Data, err := sketch2.MarshalBinary()
		require.NoError(t, err)

		// Create base label
		base, err := unmarshalDetectedLabel(&logproto.DetectedLabel{
			Label:  "app",
			Sketch: sketch1Data,
		})
		require.NoError(t, err)

		// Merge second label
		err = base.Merge(&logproto.DetectedLabel{
			Label:  "app",
			Sketch: sketch2Data,
		})
		require.NoError(t, err)

		// Expected cardinality should be ~150 (100 + 50 new values)
		require.InDelta(t, 150, base.Sketch.Estimate(), 5.0) // Allow for HLL estimation error
	})

	t.Run("merge invalid sketch", func(t *testing.T) {
		// Create valid base sketch
		sketch := hyperloglog.New()
		sketch.Insert([]byte("value1"))
		sketchData, err := sketch.MarshalBinary()
		require.NoError(t, err)

		base, err := unmarshalDetectedLabel(&logproto.DetectedLabel{
			Label:  "app",
			Sketch: sketchData,
		})
		require.NoError(t, err)

		// Try to merge invalid sketch
		err = base.Merge(&logproto.DetectedLabel{
			Label:  "app",
			Sketch: []byte("invalid sketch data"),
		})
		require.Error(t, err)
	})
}

func TestMergeLabels(t *testing.T) {
	t.Run("merge multiple labels with no overlap", func(t *testing.T) {
		var labels []*logproto.DetectedLabel

		// Create sketches for different labels
		labelNames := []string{"app", "env", "service"}
		for _, name := range labelNames {
			sketch := hyperloglog.New()
			for i := 0; i < 100; i++ {
				sketch.Insert([]byte(name + "-value-" + string(rune(i))))
			}
			sketchData, err := sketch.MarshalBinary()
			require.NoError(t, err)

			labels = append(labels, &logproto.DetectedLabel{
				Label:  name,
				Sketch: sketchData,
			})
		}

		result, err := MergeLabels(labels)
		require.NoError(t, err)
		require.Len(t, result, 3)

		// Verify each label has expected cardinality
		for _, label := range result {
			require.InDelta(t, 100, label.Cardinality, 2.0) // Allow for HLL estimation error
		}
	})

	t.Run("merge labels with overlapping values", func(t *testing.T) {
		var labels []*logproto.DetectedLabel

		// Create first app label sketch
		sketch1 := hyperloglog.New()
		for i := 0; i < 100; i++ {
			sketch1.Insert([]byte("app-value-" + string(rune(i))))
		}
		sketch1Data, err := sketch1.MarshalBinary()
		require.NoError(t, err)

		// Create second app label sketch with overlap
		sketch2 := hyperloglog.New()
		for i := 50; i < 150; i++ { // 50 overlapping values
			sketch2.Insert([]byte("app-value-" + string(rune(i))))
		}
		sketch2Data, err := sketch2.MarshalBinary()
		require.NoError(t, err)

		labels = append(labels,
			&logproto.DetectedLabel{
				Label:  "app",
				Sketch: sketch1Data,
			},
			&logproto.DetectedLabel{
				Label:  "app",
				Sketch: sketch2Data,
			},
		)

		result, err := MergeLabels(labels)
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, "app", result[0].Label)
		require.InDelta(t, 150, result[0].Cardinality, 5.0) // Allow for HLL estimation error
	})

	t.Run("merge with invalid sketch data", func(t *testing.T) {
		labels := []*logproto.DetectedLabel{
			{
				Label:  "app",
				Sketch: []byte("invalid sketch data"),
			},
		}

		_, err := MergeLabels(labels)
		require.Error(t, err)
	})

	t.Run("merge empty label list", func(t *testing.T) {
		result, err := MergeLabels(nil)
		require.NoError(t, err)
		require.Empty(t, result)

		result, err = MergeLabels([]*logproto.DetectedLabel{})
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("different label names with identical sketches", func(t *testing.T) {
		sketch := hyperloglog.New()
		for i := 0; i < 100; i++ {
			sketch.Insert([]byte("value-" + string(rune(i))))
		}
		sketchData, err := sketch.MarshalBinary()
		require.NoError(t, err)

		labels := []*logproto.DetectedLabel{
			{Label: "label1", Sketch: sketchData},
			{Label: "label2", Sketch: sketchData},
		}

		result, err := MergeLabels(labels)
		require.NoError(t, err)
		require.Len(t, result, 2)
		require.NotEqual(t, result[0].Label, result[1].Label)
		require.InDelta(t, result[0].Cardinality, result[1].Cardinality, 0.001)
	})

	t.Run("empty sketch data", func(t *testing.T) {
		labels := []*logproto.DetectedLabel{
			{Label: "app", Sketch: []byte{}},
		}
		_, err := MergeLabels(labels)
		require.Error(t, err)
	})

	t.Run("nil sketch data", func(t *testing.T) {
		labels := []*logproto.DetectedLabel{
			{Label: "app", Sketch: nil},
		}
		_, err := MergeLabels(labels)
		require.Error(t, err)
	})

	t.Run("empty label name", func(t *testing.T) {
		sketch := hyperloglog.New()
		sketch.Insert([]byte("value"))
		sketchData, err := sketch.MarshalBinary()
		require.NoError(t, err)

		labels := []*logproto.DetectedLabel{
			{Label: "", Sketch: sketchData},
		}
		result, err := MergeLabels(labels)
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Empty(t, result[0].Label)
	})

	t.Run("high cardinality", func(t *testing.T) {
		sketch := hyperloglog.New()
		// Insert 100k values
		for i := 0; i < 100000; i++ {
			sketch.Insert([]byte("value-" + string(rune(i))))
		}
		sketchData, err := sketch.MarshalBinary()
		require.NoError(t, err)

		labels := []*logproto.DetectedLabel{
			{Label: "app", Sketch: sketchData},
		}
		result, err := MergeLabels(labels)
		require.NoError(t, err)
		require.Len(t, result, 1)
		// HLL has ~2% error rate at high cardinality
		require.InDelta(t, 100000, result[0].Cardinality, 2000)
	})

	t.Run("case sensitive label names", func(t *testing.T) {
		sketch := hyperloglog.New()
		sketch.Insert([]byte("value"))
		sketchData, err := sketch.MarshalBinary()
		require.NoError(t, err)

		labels := []*logproto.DetectedLabel{
			{Label: "App", Sketch: sketchData},
			{Label: "app", Sketch: sketchData},
		}
		result, err := MergeLabels(labels)
		require.NoError(t, err)
		require.Len(t, result, 2) // Should treat App and app as different labels
	})
}
