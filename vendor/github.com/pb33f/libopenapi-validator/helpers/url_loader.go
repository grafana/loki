// Copyright 2023-2024 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package helpers

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// HTTPURLLoader is a type that implements the Loader interface for loading schemas from HTTP URLs.
// this change was made in jsonschema v6. The httploader package was removed and the HTTPURLLoader
// type was introduced.
// https://github.com/santhosh-tekuri/jsonschema/blob/boon/example_http_test.go
// TODO: make all this stuff configurable, right now it's all hard wired and not very flexible.
//
//	use interfaces and abstractions on all this.
type HTTPURLLoader http.Client

func (l *HTTPURLLoader) Load(url string) (any, error) {
	client := (*http.Client)(l)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s returned status code %d", url, resp.StatusCode)
	}
	defer resp.Body.Close()

	return jsonschema.UnmarshalJSON(resp.Body)
}

func NewHTTPURLLoader(insecure bool) *HTTPURLLoader {
	httpLoader := HTTPURLLoader(http.Client{
		Timeout: 15 * time.Second,
	})
	if insecure {
		httpLoader.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return &httpLoader
}

func NewCompilerLoader() jsonschema.SchemeURLLoader {
	return jsonschema.SchemeURLLoader{
		"file":  jsonschema.FileLoader{},
		"http":  NewHTTPURLLoader(false),
		"https": NewHTTPURLLoader(false),
	}
}
