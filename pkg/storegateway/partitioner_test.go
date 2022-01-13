package storegateway

import (
	"bytes"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/thanos/pkg/store"
)

func TestGapBasedPartitioner_Partition(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	p := newGapBasedPartitioner(10, reg)

	parts := p.Partition(5, func(i int) (uint64, uint64) {
		switch i {
		case 0:
			return 10, 12
		case 1:
			return 15, 18
		case 2:
			return 22, 27
		case 3:
			return 38, 41
		case 4:
			return 50, 52
		default:
			return 0, 0
		}
	})

	expected := []store.Part{
		{Start: 10, End: 27, ElemRng: [2]int{0, 3}},
		{Start: 38, End: 52, ElemRng: [2]int{3, 5}},
	}
	require.Equal(t, expected, parts)

	assert.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
		# HELP cortex_bucket_store_partitioner_requested_bytes_total Total size of byte ranges required to fetch from the storage before they are passed to the partitioner.
		# TYPE cortex_bucket_store_partitioner_requested_bytes_total counter
		cortex_bucket_store_partitioner_requested_bytes_total 15

		# HELP cortex_bucket_store_partitioner_requested_ranges_total Total number of byte ranges required to fetch from the storage before they are passed to the partitioner.
		# TYPE cortex_bucket_store_partitioner_requested_ranges_total counter
		cortex_bucket_store_partitioner_requested_ranges_total 5

		# HELP cortex_bucket_store_partitioner_expanded_bytes_total Total size of byte ranges returned by the partitioner after they've been combined together to reduce the number of bucket API calls.
		# TYPE cortex_bucket_store_partitioner_expanded_bytes_total counter
		cortex_bucket_store_partitioner_expanded_bytes_total 31

		# HELP cortex_bucket_store_partitioner_expanded_ranges_total Total number of byte ranges returned by the partitioner after they've been combined together to reduce the number of bucket API calls.
		# TYPE cortex_bucket_store_partitioner_expanded_ranges_total counter
		cortex_bucket_store_partitioner_expanded_ranges_total 2
	`)))
}
