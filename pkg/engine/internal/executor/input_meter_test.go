package executor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInputMeter(t *testing.T) {
	newRecordingMeter := func() (*inputMeter, *[]int64) {
		var reports []int64
		return newInputMeter(func(bytes int64) { reports = append(reports, bytes) }), &reports
	}

	t.Run("reports each full batch of rows when the batch fills", func(t *testing.T) {
		meter, reports := newRecordingMeter()
		for range 2*inputMeterBatchRows + 1 {
			meter.add(2)
		}

		require.Equal(t, []int64{2 * inputMeterBatchRows, 2 * inputMeterBatchRows}, *reports)
		require.Equal(t, int64(2*(2*inputMeterBatchRows+1)), meter.total())
	})

	t.Run("reports the rows of a partial batch on flush", func(t *testing.T) {
		meter, reports := newRecordingMeter()
		meter.add(3)
		meter.add(4)
		meter.flush()

		require.Equal(t, []int64{7}, *reports)
		require.Equal(t, int64(7), meter.total())
	})

	t.Run("reports nothing on flush when it counted no row after the last report", func(t *testing.T) {
		meter, reports := newRecordingMeter()
		meter.flush()
		for range inputMeterBatchRows {
			meter.add(1)
		}
		meter.flush()

		require.Equal(t, []int64{inputMeterBatchRows}, *reports)
	})
}
