package server

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"sort"
	"strings"
	"syscall"
	"text/template"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/version"
	serverww "github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/promtail/server/ui"
	"github.com/grafana/loki/pkg/promtail/targets"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

var (
	readinessProbeFailure = "Not ready: Unable to find any logs to tail. Please verify permissions, volumes, scrape_config, etc."
	readinessProbeSuccess = []byte("Ready")
)

type Server interface {
	Shutdown()
	Run() error
}

// Server embed weaveworks server with static file and templating capability
type server struct {
	*serverww.Server
	log               log.Logger
	tms               *targets.TargetManagers
	externalURL       *url.URL
	healthCheckTarget bool
}

// Config extends weaveworks server config
type Config struct {
	serverww.Config   `yaml:",inline"`
	ExternalURL       string `yaml:"external_url"`
	HealthCheckTarget *bool  `yaml:"health_check_target"`
	Disable           bool   `yaml:"disable"`
}

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	// NOTE: weaveworks server's config can't be registered with a prefix.
	cfg.Config.RegisterFlags(f)

	f.BoolVar(&cfg.Disable, prefix+"server.disable", false, "Disable the http and grpc server.")
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// New makes a new Server
func New(cfg Config, log log.Logger, tms *targets.TargetManagers) (Server, error) {
	if cfg.Disable {
		return newNoopServer(log), nil
	}
	wws, err := serverww.New(cfg.Config)
	if err != nil {
		return nil, err
	}

	externalURL, err := computeExternalURL(cfg.ExternalURL, cfg.HTTPListenPort)
	if err != nil {
		return nil, errors.Wrapf(err, "parse external URL %q", cfg.ExternalURL)
	}
	cfg.PathPrefix = externalURL.Path

	healthCheckTargetFlag := true
	if cfg.HealthCheckTarget != nil {
		healthCheckTargetFlag = *cfg.HealthCheckTarget
	}

	serv := &server{
		Server:            wws,
		log:               log,
		tms:               tms,
		externalURL:       externalURL,
		healthCheckTarget: healthCheckTargetFlag,
	}

	serv.HTTP.Path("/").Handler(http.RedirectHandler(path.Join(serv.externalURL.Path, "/targets"), 303))
	serv.HTTP.Path("/ready").Handler(http.HandlerFunc(serv.ready))
	serv.HTTP.PathPrefix("/static/").Handler(http.FileServer(ui.Assets))
	serv.HTTP.Path("/service-discovery").Handler(http.HandlerFunc(serv.serviceDiscovery))
	serv.HTTP.Path("/targets").Handler(http.HandlerFunc(serv.targets))
	return serv, nil

}

// serviceDiscovery serves the service discovery page.
func (s *server) serviceDiscovery(rw http.ResponseWriter, req *http.Request) {
	var index []string
	allTarget := s.tms.AllTargets()
	for job := range allTarget {
		index = append(index, job)
	}
	sort.Strings(index)
	scrapeConfigData := struct {
		Index   []string
		Targets map[string][]target.Target
		Active  []int
		Dropped []int
		Total   []int
	}{
		Index:   index,
		Targets: make(map[string][]target.Target),
		Active:  make([]int, len(index)),
		Dropped: make([]int, len(index)),
		Total:   make([]int, len(index)),
	}
	for i, job := range scrapeConfigData.Index {
		scrapeConfigData.Targets[job] = make([]target.Target, 0, len(allTarget[job]))
		scrapeConfigData.Total[i] = len(allTarget[job])
		for _, t := range allTarget[job] {
			// Do not display more than 100 dropped targets per job to avoid
			// returning too much data to the clients.
			if target.IsDropped(t) {
				scrapeConfigData.Dropped[i]++
				if scrapeConfigData.Dropped[i] > 100 {
					continue
				}
			} else {
				scrapeConfigData.Active[i]++
			}
			scrapeConfigData.Targets[job] = append(scrapeConfigData.Targets[job], t)
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
			"numReady": func(ts []target.Target) (readies int) {
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
func (s *server) targets(rw http.ResponseWriter, req *http.Request) {
	executeTemplate(req.Context(), rw, templateOptions{
		Data: struct {
			TargetPools map[string][]target.Target
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
			"numReady": func(ts []target.Target) (readies int) {
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
func (s *server) ready(rw http.ResponseWriter, _ *http.Request) {
	if s.healthCheckTarget && !s.tms.Ready() {
		http.Error(rw, readinessProbeFailure, http.StatusInternalServerError)
		return
	}

	rw.WriteHeader(http.StatusOK)
	if _, err := rw.Write(readinessProbeSuccess); err != nil {
		level.Error(s.log).Log("msg", "error writing success message", "error", err)
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

type noopServer struct {
	log  log.Logger
	sigs chan os.Signal
}

func newNoopServer(log log.Logger) *noopServer {
	return &noopServer{
		log:  log,
		sigs: make(chan os.Signal, 1),
	}
}

func (s *noopServer) Run() error {
	signal.Notify(s.sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-s.sigs
	level.Info(s.log).Log("msg", "received shutdown signal", "sig", sig)
	return nil
}
func (s *noopServer) Shutdown() {
	s.sigs <- syscall.SIGTERM
}
