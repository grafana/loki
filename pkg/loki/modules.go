package loki

import (
	"fmt"
	"net/http"
	"strings"

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
	Overrides
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
	case Overrides:
		return "overrides"
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
		panic(fmt.Sprintf("unknown module name: %d", m))
	}
}

func (m *moduleName) Set(s string) error {
	switch strings.ToLower(s) {
	case "ring":
		*m = Ring
		return nil
	case "overrides":
		*m = Overrides
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

func (t *Loki) initOverrides() (err error) {
	t.overrides, err = validation.NewOverrides(t.cfg.LimitsConfig)
	return err
}

func (t *Loki) initDistributor() (err error) {
	t.distributor, err = distributor.New(t.cfg.Distributor, t.cfg.IngesterClient, t.ring, t.overrides)
	if err != nil {
		return
	}

	t.server.HTTP.Handle("/api/prom/push", middleware.Merge(
		t.httpAuthMiddleware,
	).Wrap(http.HandlerFunc(t.distributor.PushHandler)))

	return
}

func (t *Loki) initQuerier() (err error) {
	t.querier, err = querier.New(t.cfg.Querier, t.cfg.IngesterClient, t.ring, t.store)
	if err != nil {
		return
	}

	httpMiddleware := middleware.Merge(
		t.httpAuthMiddleware,
	)
	t.server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(t.querier.QueryHandler)))
	t.server.HTTP.Handle("/api/prom/label", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/api/prom/tail", httpMiddleware.Wrap(http.HandlerFunc(t.querier.TailHandler)))
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
	t.store, err = storage.NewStore(t.cfg.StorageConfig, t.cfg.ChunkStoreConfig, t.cfg.SchemaConfig, t.overrides)
	return
}

func (t *Loki) stopStore() error {
	t.store.Stop()
	return nil
}

// listDeps recursively gets a list of dependencies for a passed moduleName
func listDeps(m moduleName) []moduleName {
	deps := modules[m].deps
	for _, d := range modules[m].deps {
		deps = append(deps, listDeps(d)...)
	}
	return deps
}

// orderedDeps gets a list of all dependencies ordered so that items are always after any of their dependencies.
func orderedDeps(m moduleName) []moduleName {
	deps := listDeps(m)

	// get a unique list of moduleNames, with a flag for whether they have been added to our result
	uniq := map[moduleName]bool{}
	for _, dep := range deps {
		uniq[dep] = false
	}

	result := make([]moduleName, 0, len(uniq))

	// keep looping through all modules until they have all been added to the result.

	for len(result) < len(uniq) {
	OUTER:
		for name, added := range uniq {
			if added {
				continue
			}
			for _, dep := range modules[name].deps {
				// stop processing this module if one of its dependencies has
				// not been added to the result yet.
				if !uniq[dep] {
					continue OUTER
				}
			}

			// if all of the module's dependencies have been added to the result slice,
			// then we can safely add this module to the result slice as well.
			uniq[name] = true
			result = append(result, name)
		}
	}
	return result
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

	Overrides: {
		init: (*Loki).initOverrides,
	},

	Distributor: {
		deps: []moduleName{Ring, Server, Overrides},
		init: (*Loki).initDistributor,
	},

	Store: {
		deps: []moduleName{Overrides},
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
