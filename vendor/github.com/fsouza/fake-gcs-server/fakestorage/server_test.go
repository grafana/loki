// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	server := NewServer([]Object{
		{BucketName: "some-bucket", Name: "img/hi-res/party-01.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-02.jpg"},
		{BucketName: "some-bucket", Name: "img/hi-res/party-03.jpg"},
		{BucketName: "other-bucket", Name: "static/css/website.css"},
	})
	defer server.Stop()
	url := server.URL()
	if url != server.ts.URL {
		t.Errorf("wrong url returned\nwant %q\ngot  %q", server.ts.URL, url)
	}
}
