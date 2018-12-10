package loki

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	opentracing "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier"
)

type moduleName int

// The various modules that make up Loki.
const (
	Ring moduleName = iota
	Server
	Distributor
	Ingester
	Querier
	Store
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
	case Store:
		return "store"
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
	case "store":
		*m = Store
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

func (t *Loki) initServer() (err error) {
	t.server, err = server.New(t.cfg.Server)
	return
}

func (t *Loki) initRing() (err error) {
	t.ring, err = ring.New(t.cfg.Ingester.LifecyclerConfig.RingConfig)
	if err != nil {
		return
	}
	t.server.HTTP.Handle("/ring", t.ring)
	return
}

func (t *Loki) initDistributor() (err error) {
	t.distributor, err = distributor.New(t.cfg.Distributor, t.cfg.IngesterClient, t.ring)
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
		t.httpAuthMiddleware,
	).Wrap(http.HandlerFunc(t.distributor.PushHandler)))

	return
}

func (t *Loki) initQuerier() (err error) {
	t.querier, err = querier.New(t.cfg.Querier, t.cfg.IngesterClient, t.ring, t.store)
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
		t.httpAuthMiddleware,
	)
	t.server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(t.querier.QueryHandler)))
	t.server.HTTP.Handle("/api/prom/label", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	return
}

func (t *Loki) initIngester() (err error) {
	t.cfg.Ingester.LifecyclerConfig.ListenPort = &t.cfg.Server.GRPCListenPort
	t.ingester, err = ingester.New(t.cfg.Ingester, t.store)
	if err != nil {
		return
	}

	logproto.RegisterPusherServer(t.server.GRPC, t.ingester)
	logproto.RegisterQuerierServer(t.server.GRPC, t.ingester)
	grpc_health_v1.RegisterHealthServer(t.server.GRPC, t.ingester)
	t.server.HTTP.Path("/ready").Handler(http.HandlerFunc(t.ingester.ReadinessHandler))
	t.server.HTTP.Path("/flush").Handler(http.HandlerFunc(t.ingester.FlushHandler))
	return
}

func (t *Loki) stopIngester() error {
	t.ingester.Shutdown()
	return nil
}

func (t *Loki) initStore() (err error) {
	var overrides *validation.Overrides
	overrides, err = validation.NewOverrides(t.cfg.LimitsConfig)
	if err != nil {
		return err
	}

	t.store, err = storage.NewStore(t.cfg.StorageConfig, t.cfg.ChunkStoreConfig, t.cfg.SchemaConfig, overrides)
	return
}

func (t *Loki) stopStore() error {
	t.store.Stop()
	return nil
}

type module struct {
	deps []moduleName
	init func(t *Loki) error
	stop func(t *Loki) error
}

var modules = map[moduleName]module{
	Server: {
		init: (*Loki).initServer,
	},

	Ring: {
		deps: []moduleName{Server},
		init: (*Loki).initRing,
	},

	Distributor: {
		deps: []moduleName{Ring, Server},
		init: (*Loki).initDistributor,
	},

	Store: {
		init: (*Loki).initStore,
		stop: (*Loki).stopStore,
	},

	Ingester: {
		deps: []moduleName{Store, Server},
		init: (*Loki).initIngester,
		stop: (*Loki).stopIngester,
	},

	Querier: {
		deps: []moduleName{Store, Ring, Server},
		init: (*Loki).initQuerier,
	},

	All: {
		deps: []moduleName{Querier, Ingester, Distributor},
	},
}
