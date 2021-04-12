package retention

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_UserSeries(t *testing.T) {
	m := newUserSeriesMap()

	m.Add([]byte(`series1`), []byte(`user1`))
	m.Add([]byte(`series1`), []byte(`user1`))
	m.Add([]byte(`series1`), []byte(`user2`))
	m.Add([]byte(`series2`), []byte(`user1`))
	m.Add([]byte(`series2`), []byte(`user1`))
	m.Add([]byte(`series2`), []byte(`user2`))

	keys := []string{}

	err := m.ForEach(func(seriesID, userID []byte) error {
		keys = append(keys, string(seriesID)+":"+string(userID))
		return nil
	})
	require.NoError(t, err)
	require.Len(t, keys, 4)
	sort.Strings(keys)
	require.Equal(t, []string{
		"series1:user1",
		"series1:user2",
		"series2:user1",
		"series2:user2",
	}, keys)
}
