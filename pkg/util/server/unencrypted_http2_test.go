package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnableUnencryptedHTTP2(t *testing.T) {
	t.Run("nil protocols", func(t *testing.T) {
		srv := &http.Server{}
		EnableUnencryptedHTTP2(srv)
		require.NotNil(t, srv.Protocols)
		require.True(t, srv.Protocols.HTTP1())
		require.True(t, srv.Protocols.UnencryptedHTTP2())
	})

	t.Run("existing protocols", func(t *testing.T) {
		srv := &http.Server{Protocols: new(http.Protocols)}
		EnableUnencryptedHTTP2(srv)
		require.True(t, srv.Protocols.HTTP1())
		require.True(t, srv.Protocols.UnencryptedHTTP2())
	})
}
