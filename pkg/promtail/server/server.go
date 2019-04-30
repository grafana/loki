package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"github.com/prometheus/common/version"
	serverww "github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/promtail/server/ui"
	"github.com/grafana/loki/pkg/promtail/targets"
)

// Server embed weaveworks server with static file and templating capability
type Server struct {
	*serverww.Server
	tms         *targets.TargetManagers
	externalURL *url.URL
}

// Config extends weaveworks server config
type Config struct {
	serverww.Config `yaml:",inline"`
	ExternalURL     string `yaml:"external_url"`
}

// New makes a new Server
func New(cfg Config, tms *targets.TargetManagers) (*Server, error) {
	wws, err := serverww.New(cfg.Config)
	if err != nil {
		return nil, err
	}

	externalURL, err := computeExternalURL(cfg.ExternalURL, cfg.HTTPListenPort)
	if err != nil {
		return nil, errors.Wrapf(err, "parse external URL %q", cfg.ExternalURL)
	}
	serv := &Server{
		Server:      wws,
		tms:         tms,
		externalURL: externalURL,
	}

	serv.HTTP.Path("/ready").Handler(http.HandlerFunc(serv.ready))
	serv.HTTP.PathPrefix("/static/").Handler(http.FileServer(ui.Assets))
	serv.HTTP.Path("/targets").Handler(http.HandlerFunc(serv.targets))
	return serv, nil

}

// targets serves the targets page.
func (s *Server) targets(rw http.ResponseWriter, _ *http.Request) {
	executeTemplate(context.Background(), rw, templateOptions{
		Data: struct {
			TargetPools map[string][]targets.Target
		}{
			TargetPools: s.tms.TargetsActive(),
		},
		BuildVersion: version.Info(),
		Name:         "targets.html",
		PageTitle:    "Targets",
		ExternalURL:  s.externalURL,
		TemplateFuncs: template.FuncMap{
			"fileTargetDetails": func(details interface{}) map[string]int64 {
				// you can't cast with a text template in go so this is a helper
				return details.(map[string]int64)
			},
			"numReady": func(ts []targets.Target) (readies int) {
				for _, t := range ts {
					if t.Ready() {
						readies++
					}
				}
				return
			},
		},
	})
}

// ready serves the ready endpoint
func (s *Server) ready(rw http.ResponseWriter, _ *http.Request) {
	if s.tms.Ready() {
		rw.WriteHeader(http.StatusNoContent)
	} else {
		rw.WriteHeader(http.StatusInternalServerError)
	}
}

// computeExternalURL computes a sanitized external URL from a raw input. It infers unset
// URL parts from the OS and the given listen address.
func computeExternalURL(u string, port int) (*url.URL, error) {
	if u == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, err
		}

		u = fmt.Sprintf("http://%s:%d/", hostname, port)
	}

	eu, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	ppref := strings.TrimRight(eu.Path, "/")
	if ppref != "" && !strings.HasPrefix(ppref, "/") {
		ppref = "/" + ppref
	}
	eu.Path = ppref

	return eu, nil
}
