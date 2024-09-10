// Copyright 2019 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"net/http"
	"net/http/httptest"

	"github.com/gorilla/mux"
)

type muxTransport struct {
	router *mux.Router
}

func (t *muxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.router.ServeHTTP(w, r)
	return w.Result(), nil
}
