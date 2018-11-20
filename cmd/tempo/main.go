package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	opentracing "github.com/opentracing/opentracing-go"
	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/tempo/pkg/distributor"
	"github.com/grafana/tempo/pkg/flagext"
	"github.com/grafana/tempo/pkg/ingester"
	"github.com/grafana/tempo/pkg/ingester/client"
	"github.com/grafana/tempo/pkg/logproto"
	"github.com/grafana/tempo/pkg/querier"
)

type config struct {
	serverConfig         server.Config
	distributorConfig    distributor.Config
	ingesterConfig       ingester.Config
	querierConfig        querier.Config
	ingesterClientConfig client.Config
}

func (c *config) RegisterFlags(f *flag.FlagSet) {
	c.serverConfig.MetricsNamespace = "tempo"
	c.serverConfig.GRPCMiddleware = []grpc.UnaryServerInterceptor{
		middleware.ServerUserHeaderInterceptor,
	}

	flagext.RegisterConfigs(f, &c.serverConfig, &c.distributorConfig,
		&c.ingesterConfig, &c.querierConfig, &c.ingesterClientConfig)
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
	Ring: module{
		init: func(t *Tempo, cfg *config) (err error) {
			t.ring, err = ring.New(cfg.ingesterConfig.LifecyclerConfig.RingConfig)
			if err != nil {
				return
			}
			t.server.HTTP.Handle("/ring", t.ring)
			return
		},
	},

	Server: module{
		init: func(t *Tempo, cfg *config) (err error) {
			t.server, err = server.New(cfg.serverConfig)
			return
		},
		stop: func(t *Tempo) {
			t.server.Shutdown()
		},
	},

	Distributor: module{
		deps: []moduleName{Ring, Server},
		init: func(t *Tempo, cfg *config) (err error) {
			t.distributor, err = distributor.New(cfg.distributorConfig, cfg.ingesterClientConfig, t.ring)
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
			cfg.ingesterConfig.LifecyclerConfig.ListenPort = &cfg.serverConfig.GRPCListenPort
			t.ingester, err = ingester.New(cfg.ingesterConfig)
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
			t.querier, err = querier.New(cfg.querierConfig, cfg.ingesterClientConfig, t.ring)
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

func initModule(m moduleName) {
	if _, ok := inited[m]; ok {
		return
	}

	for _, dep := range modules[m].deps {
		initModule(dep)
	}

	if err := modules[m].init(&tempo, &cfg); err != nil {
		log.Fatalf("Error initializing %s: %v", m, err)
	}

	inited[m] = struct{}{}
}

func main() {
	flagset := flag.NewFlagSet("", flag.ExitOnError)
	target := All
	flagset.Var(&target, "target", "target module (default All)")
	flagext.RegisterConfigs(flagset, &cfg)
	flagset.Parse(os.Args[1:])

	util.InitLogger(&cfg.serverConfig)

	initModule(target)
	tempo.server.Run()
	tempo.server.Shutdown()
}
