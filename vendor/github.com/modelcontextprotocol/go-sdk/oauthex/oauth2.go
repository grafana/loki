// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Package oauthex implements extensions to OAuth2.

//go:build mcp_go_client_oauth

package oauthex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// prependToPath prepends pre to the path of urlStr.
// When pre is the well-known path, this is the algorithm specified in both RFC 9728
// section 3.1 and RFC 8414 section 3.1.
func prependToPath(urlStr, pre string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	p := "/" + strings.Trim(pre, "/")
	if u.Path != "" {
		p += "/"
	}

	u.Path = p + strings.TrimLeft(u.Path, "/")
	return u.String(), nil
}

// getJSON retrieves JSON and unmarshals JSON from the URL, as specified in both
// RFC 9728 and RFC 8414.
// It will not read more than limit bytes from the body.
func getJSON[T any](ctx context.Context, c *http.Client, url string, limit int64) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	// Specs require a 200.
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status %s", res.Status)
	}
	// Specs require application/json.
	ct := res.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		return nil, fmt.Errorf("bad content type %q", ct)
	}

	var t T
	dec := json.NewDecoder(io.LimitReader(res.Body, limit))
	if err := dec.Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// checkURLScheme ensures that its argument is a valid URL with a scheme
// that prevents XSS attacks.
// See #526.
func checkURLScheme(u string) error {
	if u == "" {
		return nil
	}
	uu, err := url.Parse(u)
	if err != nil {
		return err
	}
	scheme := strings.ToLower(uu.Scheme)
	if scheme == "javascript" || scheme == "data" || scheme == "vbscript" {
		return fmt.Errorf("URL has disallowed scheme %q", scheme)
	}
	return nil
}
