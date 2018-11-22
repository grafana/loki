package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/go-kit/kit/log/level"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"

	"github.com/grafana/tempo/pkg/distributor"
	"github.com/grafana/tempo/pkg/ingester"
	"github.com/grafana/tempo/pkg/ingester/client"
	"github.com/grafana/tempo/pkg/logproto"
	"github.com/grafana/tempo/pkg/querier"
)

type config struct {
	Server         server.Config      `yaml:"server,omitempty"`
	Distributor    distributor.Config `yaml:"distributor,omitempty"`
	Querier        querier.Config     `yaml:"querier,omitempty"`
	IngesterClient client.Config      `yaml:"ingester_client,omitempty"`
	Ingester       ingester.Config    `yaml:"ingester,omitempty"`
}

func (c *config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "tempo"
	c.Server.GRPCMiddleware = []grpc.UnaryServerInterceptor{
		middleware.ServerUserHeaderInterceptor,
	}
	c.Server.GRPCStreamMiddleware = []grpc.StreamServerInterceptor{
		middleware.StreamServerUserHeaderInterceptor,
	}

	c.Server.RegisterFlags(f)
	c.Distributor.RegisterFlags(f)
	c.Querier.RegisterFlags(f)
	c.IngesterClient.RegisterFlags(f)
	c.Ingester.RegisterFlags(f)
}

type Tempo struct {
	server      *server.Server
	ring        *ring.Ring
	distributor *distributor.Distributor
	ingester    *ingester.Ingester
	querier     *querier.Querier
}

type moduleName int

const (
	Ring moduleName = iota
	Server
	Distributor
	Ingester
	Querier
	All
)

func (m moduleName) String() string {
	switch m {
	case Ring:
		return "ring"
	case Server:
		return "server"
	case Distributor:
		return "distributor"
	case Ingester:
		return "ingester"
	case Querier:
		return "querier"
	case All:
		return "all"
	default:
		panic(fmt.Sprintf("unknow module name: %d", m))
	}
}

func (m *moduleName) Set(s string) error {
	switch strings.ToLower(s) {
	case "ring":
		*m = Ring
		return nil
	case "server":
		*m = Server
		return nil
	case "distributor":
		*m = Distributor
		return nil
	case "ingester":
		*m = Ingester
		return nil
	case "querier":
		*m = Querier
		return nil
	case "all":
		*m = All
		return nil
	default:
		return fmt.Errorf("unrecognised module name: %s", s)
	}
}

type module struct {
	deps []moduleName
	init func(t *Tempo, cfg *config) error
	stop func(t *Tempo)
}

var modules = map[moduleName]module{
	Server: module{
		init: func(t *Tempo, cfg *config) (err error) {
			t.server, err = server.New(cfg.Server)
			return
		},
	},

	Ring: module{
		deps: []moduleName{Server},
		init: func(t *Tempo, cfg *config) (err error) {
			t.ring, err = ring.New(cfg.Ingester.LifecyclerConfig.RingConfig)
			if err != nil {
				return
			}
			t.server.HTTP.Handle("/ring", t.ring)
			return
		},
	},

	Distributor: module{
		deps: []moduleName{Ring, Server},
		init: func(t *Tempo, cfg *config) (err error) {
			t.distributor, err = distributor.New(cfg.Distributor, cfg.IngesterClient, t.ring)
			if err != nil {
				return
			}

			operationNameFunc := nethttp.OperationNameFunc(func(r *http.Request) string {
				return r.URL.RequestURI()
			})
			t.server.HTTP.Handle("/api/prom/push", middleware.Merge(
				middleware.Func(func(handler http.Handler) http.Handler {
					return nethttp.Middleware(opentracing.GlobalTracer(), handler, operationNameFunc)
				}),
				middleware.AuthenticateUser,
			).Wrap(http.HandlerFunc(t.distributor.PushHandler)))

			return
		},
	},

	Ingester: module{
		deps: []moduleName{Server},
		init: func(t *Tempo, cfg *config) (err error) {
			cfg.Ingester.LifecyclerConfig.ListenPort = &cfg.Server.GRPCListenPort
			t.ingester, err = ingester.New(cfg.Ingester)
			if err != nil {
				return
			}

			logproto.RegisterPusherServer(t.server.GRPC, t.ingester)
			logproto.RegisterQuerierServer(t.server.GRPC, t.ingester)
			grpc_health_v1.RegisterHealthServer(t.server.GRPC, t.ingester)
			t.server.HTTP.Path("/ready").Handler(http.HandlerFunc(t.ingester.ReadinessHandler))
			return
		},
		stop: func(t *Tempo) {
			t.ingester.Shutdown()
		},
	},

	Querier: module{
		deps: []moduleName{Ring, Server},
		init: func(t *Tempo, cfg *config) (err error) {
			t.querier, err = querier.New(cfg.Querier, cfg.IngesterClient, t.ring)
			if err != nil {
				return
			}

			operationNameFunc := nethttp.OperationNameFunc(func(r *http.Request) string {
				return r.URL.RequestURI()
			})
			httpMiddleware := middleware.Merge(
				middleware.Func(func(handler http.Handler) http.Handler {
					return nethttp.Middleware(opentracing.GlobalTracer(), handler, operationNameFunc)
				}),
				middleware.AuthenticateUser,
			)
			t.server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(t.querier.QueryHandler)))
			t.server.HTTP.Handle("/api/prom/label", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
			t.server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
			return
		},
	},

	All: module{
		deps: []moduleName{Querier, Ingester, Distributor},
		init: func(t *Tempo, cfg *config) (err error) {
			return
		},
	},
}

var (
	cfg    config
	tempo  Tempo
	inited = map[moduleName]struct{}{}
)

func initModule(m moduleName) error {
	if _, ok := inited[m]; ok {
		return nil
	}

	for _, dep := range modules[m].deps {
		initModule(dep)
	}

	level.Info(util.Logger).Log("msg", "initialising", "module", m)
	if err := modules[m].init(&tempo, &cfg); err != nil {
		return errors.Wrap(err, fmt.Sprintf("error initialising module: %s", m))
	}

	inited[m] = struct{}{}
	return nil
}

func stopModule(m moduleName) {
	if _, ok := inited[m]; !ok {
		return
	}
	delete(inited, m)

	for _, dep := range modules[m].deps {
		stopModule(dep)
	}

	if modules[m].stop != nil {
		level.Info(util.Logger).Log("msg", "stopping", "module", m)
		modules[m].stop(&tempo)
	}
}

func readConfig(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return errors.Wrap(err, "error opening config file")
	}
	defer f.Close()

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return errors.Wrap(err, "Error reading config file: %v")
	}

	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return errors.Wrap(err, "Error reading config file: %v")
	}
	return nil
}

func main() {
	var (
		target     = All
		configFile = ""
	)
	flag.Var(&target, "target", "target module (default All)")
	flag.StringVar(&configFile, "config.file", "", "Configuration file to load.")
	flagext.RegisterFlags(&cfg)
	flag.Parse()

	util.InitLogger(&cfg.Server)

	if configFile != "" {
		if err := readConfig(configFile); err != nil {
			level.Error(util.Logger).Log("msg", "error loading config", "filename", configFile, "err", err)
			os.Exit(1)
		}
	}

	if err := initModule(target); err != nil {
		level.Error(util.Logger).Log("msg", "error initialising module", "err", err)
		os.Exit(1)
	}

	tempo.server.Run()
	tempo.server.Shutdown()
	stopModule(target)
}
