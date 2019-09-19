package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"text/template"

	logutil "github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/version"
	serverww "github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/promtail/server/ui"
	"github.com/grafana/loki/pkg/promtail/targets"
)

var (
	readinessProbeFailure = "Not ready: Unable to find any logs to tail. Please verify permissions, volumes, scrape_config, etc."
	readinessProbeSuccess = []byte("Ready")
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
	cfg.PathPrefix = externalURL.Path

	serv := &Server{
		Server:      wws,
		tms:         tms,
		externalURL: externalURL,
	}

	serv.HTTP.Path("/").Handler(http.RedirectHandler(path.Join(serv.externalURL.Path, "/targets"), 303))
	serv.HTTP.Path("/ready").Handler(http.HandlerFunc(serv.ready))
	serv.HTTP.PathPrefix("/static/").Handler(http.FileServer(ui.Assets))
	serv.HTTP.Path("/service-discovery").Handler(http.HandlerFunc(serv.serviceDiscovery))
	serv.HTTP.Path("/targets").Handler(http.HandlerFunc(serv.targets))
	return serv, nil

}

// serviceDiscovery serves the service discovery page.
func (s *Server) serviceDiscovery(rw http.ResponseWriter, req *http.Request) {
	var index []string
	allTarget := s.tms.AllTargets()
	for job := range allTarget {
		index = append(index, job)
	}
	sort.Strings(index)
	scrapeConfigData := struct {
		Index   []string
		Targets map[string][]targets.Target
		Active  []int
		Dropped []int
		Total   []int
	}{
		Index:   index,
		Targets: make(map[string][]targets.Target),
		Active:  make([]int, len(index)),
		Dropped: make([]int, len(index)),
		Total:   make([]int, len(index)),
	}
	for i, job := range scrapeConfigData.Index {
		scrapeConfigData.Targets[job] = make([]targets.Target, 0, len(allTarget[job]))
		scrapeConfigData.Total[i] = len(allTarget[job])
		for _, target := range allTarget[job] {
			// Do not display more than 100 dropped targets per job to avoid
			// returning too much data to the clients.
			if targets.IsDropped(target) {
				scrapeConfigData.Dropped[i]++
				if scrapeConfigData.Dropped[i] > 100 {
					continue
				}
			} else {
				scrapeConfigData.Active[i]++
			}
			scrapeConfigData.Targets[job] = append(scrapeConfigData.Targets[job], target)
		}
	}

	executeTemplate(req.Context(), rw, templateOptions{
		Data:         scrapeConfigData,
		BuildVersion: version.Info(),
		Name:         "service-discovery.html",
		PageTitle:    "Service Discovery",
		ExternalURL:  s.externalURL,
		TemplateFuncs: template.FuncMap{
			"fileTargetDetails": func(details interface{}) map[string]int64 {
				// you can't cast with a text template in go so this is a helper
				return details.(map[string]int64)
			},
			"dropReason": func(details interface{}) string {
				if reason, ok := details.(string); ok {
					return reason
				}
				return ""
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

// targets serves the targets page.
func (s *Server) targets(rw http.ResponseWriter, req *http.Request) {
	executeTemplate(req.Context(), rw, templateOptions{
		Data: struct {
			TargetPools map[string][]targets.Target
		}{
			TargetPools: s.tms.ActiveTargets(),
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
			"journalTargetDetails": func(details interface{}) map[string]string {
				// you can't cast with a text template in go so this is a helper
				return details.(map[string]string)
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
	if !s.tms.Ready() {
		http.Error(rw, readinessProbeFailure, http.StatusInternalServerError)
		return
	}

	rw.WriteHeader(http.StatusOK)
	if _, err := rw.Write(readinessProbeSuccess); err != nil {
		level.Error(logutil.Logger).Log("msg", "error writing success message", "error", err)
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
