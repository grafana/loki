package server

import "net/http"

// EnableUnencryptedHTTP2 configures srv to accept HTTP/1.x and cleartext HTTP/2 (h2c).
func EnableUnencryptedHTTP2(srv *http.Server) {
	protocols := srv.Protocols
	if protocols == nil {
		protocols = new(http.Protocols)
		srv.Protocols = protocols
	}
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
}
