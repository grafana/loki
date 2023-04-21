package reader

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyHeaders(t *testing.T) {
	reader := &Reader{
		user:     "user",
		pass:     "pass",
		tenantID: "tenant",
		actor:    "actor",
	}

	t.Run("apply headers to existing request", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		header := reader.ApplyHeaders(req)
		require.Equal(t, "tenant", req.Header.Get("X-Scope-OrgID"))
		require.Equal(t, "actor", req.Header.Get("X-Loki-Actor-Path"))
		require.Equal(t, "loki-canary/", req.Header.Get("User-Agent"))

		auth := reader.user + ":" + reader.pass
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)), req.Header.Get("Authorization"))

		require.Equal(t, req.Header, header)
	})

	t.Run("apply headers to nil request", func(t *testing.T) {
		header := reader.ApplyHeaders(nil)
		require.Equal(t, "tenant", header.Get("X-Scope-OrgID"))
		require.Equal(t, "actor", header.Get("X-Loki-Actor-Path"))
		require.Equal(t, "loki-canary/", header.Get("User-Agent"))

		auth := reader.user + ":" + reader.pass
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)), header.Get("Authorization"))
	})

	t.Run("only apply headers for existing fields", func(t *testing.T) {
		reader := &Reader{}
		header := reader.ApplyHeaders(nil)
		keys := []string{}
		for k := range header {
			keys = append(keys, k)
		}
		require.Equal(t, []string{"User-Agent"}, keys)
	})

}
