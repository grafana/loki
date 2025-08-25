// Copyright 2019 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"errors"
	"net/http"
	"net/http/httptest"
)

type muxTransport struct {
	handler http.Handler
	closed  bool
}

func (t *muxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.closed {
		return nil, errors.New("server closed")
	}
	w := httptest.NewRecorder()
	t.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
