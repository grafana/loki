package executor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestPostingsDedup(t *testing.T) {
	row := func(objectPath, labelValue string, minTimestamp, maxTimestamp int64) postings.Row {
		return postings.Row{
			Kind: postings.KindLabel, ObjectPath: objectPath, ColumnName: "service", LabelValue: labelValue,
			MinTimestamp: minTimestamp, MaxTimestamp: maxTimestamp,
		}
	}

	t.Run("keeps rows with different identities", func(t *testing.T) {
		var dedup postingsDedup
		for _, r := range []postings.Row{row("log-A", "api", 1, 2), row("log-B", "api", 1, 2), row("log-A", "web", 1, 2)} {
			duplicate, err := dedup.add(r)
			require.NoError(t, err)
			require.False(t, duplicate)
		}
	})

	t.Run("reports a row equal to the previous row as a duplicate", func(t *testing.T) {
		var dedup postingsDedup
		_, err := dedup.add(row("log-A", "api", 1, 2))
		require.NoError(t, err)

		duplicate, err := dedup.add(row("log-A", "api", 1, 2))
		require.NoError(t, err)
		require.True(t, duplicate)
	})

	t.Run("returns an error when the next row repeats an identity with a different time range", func(t *testing.T) {
		var dedup postingsDedup
		_, err := dedup.add(row("log-A", "api", 1, 2))
		require.NoError(t, err)

		_, err = dedup.add(row("log-A", "api", 1, 3))
		require.ErrorContains(t, err, "different time ranges [1, 2] and [1, 3]")
	})

	t.Run("returns an error when a later row in the same posting key repeats an identity with a different time range", func(t *testing.T) {
		var dedup postingsDedup
		for _, r := range []postings.Row{row("log-A", "api", 1, 2), row("log-B", "api", 2, 3)} {
			_, err := dedup.add(r)
			require.NoError(t, err)
		}

		_, err := dedup.add(row("log-A", "api", 3, 4))
		require.ErrorContains(t, err, `object "log-A", section 0 have different time ranges`)
	})

	t.Run("keeps a row of a new posting key that has the object and section of an earlier key", func(t *testing.T) {
		var dedup postingsDedup
		_, err := dedup.add(row("log-A", "api", 1, 2))
		require.NoError(t, err)

		duplicate, err := dedup.add(row("log-A", "web", 1, 2))
		require.NoError(t, err)
		require.False(t, duplicate)
	})

	t.Run("forgets a long run and finds repeats in the short runs after it", func(t *testing.T) {
		var dedup postingsDedup
		longRun := 10 * postingsDedupShrinkFactor
		for i := range longRun {
			_, err := dedup.add(row(fmt.Sprintf("log-%05d", i), "api", 1, 2))
			require.NoError(t, err)
		}
		for _, labelValue := range []string{"auth", "web"} {
			duplicate, err := dedup.add(row("log-00000", labelValue, 1, 2))
			require.NoError(t, err)
			require.False(t, duplicate)

			duplicate, err = dedup.add(row("log-00000", labelValue, 1, 2))
			require.NoError(t, err)
			require.True(t, duplicate)
		}
	})
}
