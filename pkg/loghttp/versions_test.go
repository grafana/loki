package loghttp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_GetVersion(t *testing.T) {
	require.Equal(t, GetVersion("/loki/api/v1/query_range"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/query"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/labels"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/label/{name}/values"), VersionV1)
	require.Equal(t, GetVersion("/loki/api/v1/tail"), VersionV1)

	require.Equal(t, GetVersion("/LOKI/api/v1/query_range"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/query"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/labels"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/label/{name}/values"), VersionV1)
	require.Equal(t, GetVersion("/LOKI/api/v1/tail"), VersionV1)
}
