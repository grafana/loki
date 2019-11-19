package loghttp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_GetVersion(t *testing.T) {
	require.Equal(t, GetVersion("/loki/api/v1/query_range"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/query"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/label"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/label/{name}/values"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/tail"), VersionV1)

	require.Equal(t, GetVersion("/api/prom/query"), VersionLegacy)
	require.Equal(t, GetVersion("/api/prom/label"), VersionLegacy)
	require.Equal(t, GetVersion("/api/prom/label/{name}/values"), VersionLegacy)
	require.Equal(t, GetVersion("/api/prom/tail"), VersionLegacy)

	require.Equal(t, GetVersion("/LOKI/api/v1/query_range"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/query"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/label"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/label/{name}/values"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/tail"), VersionV1)

	require.Equal(t, GetVersion("/API/prom/query"), VersionLegacy)
	require.Equal(t, GetVersion("/API/prom/label"), VersionLegacy)
	require.Equal(t, GetVersion("/API/prom/label/{name}/values"), VersionLegacy)
	require.Equal(t, GetVersion("/API/prom/tail"), VersionLegacy)

	require.NotEqual(t, int64(VersionV1), int64(VersionLegacy))
}
