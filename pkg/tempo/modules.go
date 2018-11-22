package tempo

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	opentracing "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"

	"github.com/grafana/tempo/pkg/distributor"
	"github.com/grafana/tempo/pkg/ingester"
	"github.com/grafana/tempo/pkg/logproto"
	"github.com/grafana/tempo/pkg/querier"
)

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

func (t *Tempo) initServer() (err error) {
	t.server, err = server.New(t.cfg.Server)
	return
}

func (t *Tempo) initRing() (err error) {
	t.ring, err = ring.New(t.cfg.Ingester.LifecyclerConfig.RingConfig)
	if err != nil {
		return
	}
	t.server.HTTP.Handle("/ring", t.ring)
	return
}

func (t *Tempo) initDistributor() (err error) {
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

func (t *Tempo) initQuerier() (err error) {
	t.querier, err = querier.New(t.cfg.Querier, t.cfg.IngesterClient, t.ring)
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

func (t *Tempo) initIngester() (err error) {
	t.cfg.Ingester.LifecyclerConfig.ListenPort = &t.cfg.Server.GRPCListenPort
	t.ingester, err = ingester.New(t.cfg.Ingester)
	if err != nil {
		return
	}

	logproto.RegisterPusherServer(t.server.GRPC, t.ingester)
	logproto.RegisterQuerierServer(t.server.GRPC, t.ingester)
	grpc_health_v1.RegisterHealthServer(t.server.GRPC, t.ingester)
	t.server.HTTP.Path("/ready").Handler(http.HandlerFunc(t.ingester.ReadinessHandler))
	return
}

func (t *Tempo) stopIngester() {
	t.ingester.Shutdown()
}

type module struct {
	deps []moduleName
	init func(t *Tempo) error
	stop func(t *Tempo)
}

var modules = map[moduleName]module{
	Server: module{
		init: (*Tempo).initServer,
	},

	Ring: module{
		deps: []moduleName{Server},
		init: (*Tempo).initRing,
	},

	Distributor: module{
		deps: []moduleName{Ring, Server},
		init: (*Tempo).initDistributor,
	},

	Ingester: module{
		deps: []moduleName{Server},
		init: (*Tempo).initIngester,
		stop: (*Tempo).stopIngester,
	},

	Querier: module{
		deps: []moduleName{Ring, Server},
		init: (*Tempo).initQuerier,
	},

	All: module{
		deps: []moduleName{Querier, Ingester, Distributor},
	},
}
