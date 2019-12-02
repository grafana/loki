package loki

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"

	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier"
	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util/validation"
)

const maxChunkAgeForTableManager = 12 * time.Hour

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
	TableManager
	All
)

func (m *moduleName) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var val string
	if err := unmarshal(&val); err != nil {
		return err
	}

	return m.Set(val)
}

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
	case TableManager:
		return "table-manager"
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
	case "table-manager":
		*m = TableManager
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
	t.ring, err = ring.New(t.cfg.Ingester.LifecyclerConfig.RingConfig, "ingester")
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

	pushHandler := middleware.Merge(
		t.httpAuthMiddleware,
	).Wrap(http.HandlerFunc(t.distributor.PushHandler))

	t.server.HTTP.Path("/ready").Handler(http.HandlerFunc(t.distributor.ReadinessHandler))

	t.server.HTTP.Handle("/api/prom/push", pushHandler)
	t.server.HTTP.Handle("/loki/api/v1/push", pushHandler)
	return
}

func (t *Loki) stopDistributor() (err error) {
	t.distributor.Stop()
	return nil
}

func (t *Loki) initQuerier() (err error) {
	t.querier, err = querier.New(t.cfg.Querier, t.cfg.IngesterClient, t.ring, t.store, t.overrides)
	if err != nil {
		return
	}

	httpMiddleware := middleware.Merge(
		t.httpAuthMiddleware,
	)
	t.server.HTTP.Path("/ready").Handler(http.HandlerFunc(t.querier.ReadinessHandler))

	t.server.HTTP.Handle("/loki/api/v1/query_range", httpMiddleware.Wrap(http.HandlerFunc(t.querier.RangeQueryHandler)))
	t.server.HTTP.Handle("/loki/api/v1/query", httpMiddleware.Wrap(http.HandlerFunc(t.querier.InstantQueryHandler)))
	t.server.HTTP.Handle("/loki/api/v1/label", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/loki/api/v1/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/loki/api/v1/tail", httpMiddleware.Wrap(http.HandlerFunc(t.querier.TailHandler)))

	t.server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LogQueryHandler)))
	t.server.HTTP.Handle("/api/prom/label", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/api/prom/tail", httpMiddleware.Wrap(http.HandlerFunc(t.querier.TailHandler)))
	return
}

func (t *Loki) initIngester() (err error) {
	t.cfg.Ingester.LifecyclerConfig.ListenPort = &t.cfg.Server.GRPCListenPort
	t.ingester, err = ingester.New(t.cfg.Ingester, t.cfg.IngesterClient, t.store, t.overrides)
	if err != nil {
		return
	}

	logproto.RegisterPusherServer(t.server.GRPC, t.ingester)
	logproto.RegisterQuerierServer(t.server.GRPC, t.ingester)
	logproto.RegisterIngesterServer(t.server.GRPC, t.ingester)
	grpc_health_v1.RegisterHealthServer(t.server.GRPC, t.ingester)
	t.server.HTTP.Path("/ready").Handler(http.HandlerFunc(t.ingester.ReadinessHandler))
	t.server.HTTP.Path("/flush").Handler(http.HandlerFunc(t.ingester.FlushHandler))
	return
}

func (t *Loki) stopIngester() error {
	t.ingester.Shutdown()
	return nil
}

func (t *Loki) stoppingIngester() error {
	t.ingester.Stopping()
	return nil
}

func (t *Loki) initTableManager() error {
	err := t.cfg.SchemaConfig.Load()
	if err != nil {
		return err
	}

	// Assume the newest config is the one to use
	lastConfig := &t.cfg.SchemaConfig.Configs[len(t.cfg.SchemaConfig.Configs)-1]

	if (t.cfg.TableManager.ChunkTables.WriteScale.Enabled ||
		t.cfg.TableManager.IndexTables.WriteScale.Enabled ||
		t.cfg.TableManager.ChunkTables.InactiveWriteScale.Enabled ||
		t.cfg.TableManager.IndexTables.InactiveWriteScale.Enabled ||
		t.cfg.TableManager.ChunkTables.ReadScale.Enabled ||
		t.cfg.TableManager.IndexTables.ReadScale.Enabled ||
		t.cfg.TableManager.ChunkTables.InactiveReadScale.Enabled ||
		t.cfg.TableManager.IndexTables.InactiveReadScale.Enabled) &&
		(t.cfg.StorageConfig.AWSStorageConfig.ApplicationAutoScaling.URL == nil && t.cfg.StorageConfig.AWSStorageConfig.Metrics.URL == "") {
		level.Error(util.Logger).Log("msg", "WriteScale is enabled but no ApplicationAutoScaling or Metrics URL has been provided")
		os.Exit(1)
	}

	tableClient, err := storage.NewTableClient(lastConfig.IndexType, t.cfg.StorageConfig.Config)
	if err != nil {
		return err
	}

	bucketClient, err := storage.NewBucketClient(t.cfg.StorageConfig.Config)
	util.CheckFatal("initializing bucket client", err)

	t.tableManager, err = chunk.NewTableManager(t.cfg.TableManager, t.cfg.SchemaConfig, maxChunkAgeForTableManager, tableClient, bucketClient)
	if err != nil {
		return err
	}

	t.tableManager.Start()
	return nil
}

func (t *Loki) stopTableManager() error {
	t.tableManager.Stop()
	return nil
}

func (t *Loki) initStore() (err error) {
	t.store, err = loki_storage.NewStore(t.cfg.StorageConfig, t.cfg.ChunkStoreConfig, t.cfg.SchemaConfig, t.overrides)
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
	// get a unique list of dependencies and init a map to keep whether they have been added to our result
	deps := uniqueDeps(listDeps(m))
	added := map[moduleName]bool{}

	result := make([]moduleName, 0, len(deps))

	// keep looping through all modules until they have all been added to the result.
	for len(result) < len(deps) {
	OUTER:
		for _, name := range deps {
			if added[name] {
				continue
			}

			for _, dep := range modules[name].deps {
				// stop processing this module if one of its dependencies has
				// not been added to the result yet.
				if !added[dep] {
					continue OUTER
				}
			}

			// if all of the module's dependencies have been added to the result slice,
			// then we can safely add this module to the result slice as well.
			added[name] = true
			result = append(result, name)
		}
	}

	return result
}

// uniqueDeps returns the unique list of input dependencies, guaranteeing input order stability
func uniqueDeps(deps []moduleName) []moduleName {
	result := make([]moduleName, 0, len(deps))
	uniq := map[moduleName]bool{}

	for _, dep := range deps {
		if !uniq[dep] {
			result = append(result, dep)
			uniq[dep] = true
		}
	}

	return result
}

type module struct {
	deps     []moduleName
	init     func(t *Loki) error
	stopping func(t *Loki) error
	stop     func(t *Loki) error
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
		stop: (*Loki).stopDistributor,
	},

	Store: {
		deps: []moduleName{Overrides},
		init: (*Loki).initStore,
		stop: (*Loki).stopStore,
	},

	Ingester: {
		deps:     []moduleName{Store, Server},
		init:     (*Loki).initIngester,
		stop:     (*Loki).stopIngester,
		stopping: (*Loki).stoppingIngester,
	},

	Querier: {
		deps: []moduleName{Store, Ring, Server},
		init: (*Loki).initQuerier,
	},

	TableManager: {
		deps: []moduleName{Server},
		init: (*Loki).initTableManager,
		stop: (*Loki).stopTableManager,
	},

	All: {
		deps: []moduleName{Querier, Ingester, Distributor, TableManager},
	},
}
