package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/grafana/loki/pkg/promtail/server/ui"
	"github.com/grafana/loki/pkg/promtail/targets"
	"github.com/pkg/errors"
	serverww "github.com/weaveworks/common/server"
)

type Server struct {
	*serverww.Server
	tms         *targets.TargetManagers
	externalURL *url.URL
}

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
	serv.HTTP.Path("/ready").Handler(http.HandlerFunc(serv.ready))
	serv.HTTP.Path("/service-discovery").Handler(http.HandlerFunc(serv.targets))
	return serv, nil

}

func (s *Server) targets(rw http.ResponseWriter, req *http.Request) {
	executeTemplate(context.Background(), rw, s.externalURL, "targets.html", nil)
}

func (s *Server) ready(rw http.ResponseWriter, req *http.Request) {
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
