// Copyright 2023 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !genassets
// +build !genassets

//go:generate go run -tags genassets gen_assets.go

package web

import (
	"bytes"
	_ "embed"
	"net/http"
	"text/template"
)

// Config represents the configuration of the web listener.
type LandingConfig struct {
	HeaderColor string         // Used for the landing page header.
	CSS         string         // CSS style tag for the landing page.
	Name        string         // The name of the exporter, generally suffixed by _exporter.
	Description string         // A short description about the exporter.
	Links       []LandingLinks // Links displayed on the landing page.
	Version     string         // The version displayed.
}

type LandingLinks struct {
	Address     string // The URL the link points to.
	Text        string // The text of the link.
	Description string // A descriptive textfor the link.
}

type LandingPageHandler struct {
	landingPage []byte
}

var (
	//go:embed landing_page.html
	landingPagehtmlContent string
	//go:embed landing_page.css
	landingPagecssContent string
)

func NewLandingPage(c LandingConfig) (*LandingPageHandler, error) {
	var buf bytes.Buffer
	if c.CSS == "" {
		if c.HeaderColor == "" {
			// Default to Prometheus orange.
			c.HeaderColor = "#e6522c"
		}
		cssTemplate := template.Must(template.New("landing css").Parse(landingPagecssContent))
		if err := cssTemplate.Execute(&buf, c); err != nil {
			return nil, err
		}
		c.CSS = buf.String()
	}
	t := template.Must(template.New("landing page").Parse(landingPagehtmlContent))

	buf.Reset()
	if err := t.Execute(&buf, c); err != nil {
		return nil, err
	}

	return &LandingPageHandler{
		landingPage: buf.Bytes(),
	}, nil
}

func (h *LandingPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write(h.landingPage)
}
