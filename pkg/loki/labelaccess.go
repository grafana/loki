package loki

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/labelaccess"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func (t *Loki) setupLBAC() error {
	t.Codec = labelaccess.NewCodec(t.Codec)

	t.ModuleManager.RegisterModule(AuthMiddleware, t.initAuthMiddleware, modules.UserInvisibleModule)
	t.ModuleManager.RegisterModule(LabelAccess, t.initStoreChunkFilterer)
	t.ModuleManager.RegisterModule(LabelAccessStoreWrapper, t.initLabelAccessStoreWrapper, modules.UserInvisibleModule)
	t.ModuleManager.RegisterModule(LabelAccessIngesterWrapper, t.initLabelAccessIngesterWrapper)
	t.ModuleManager.RegisterModule(LabelAccessV2Engine, t.initLabelAccessV2Engine, modules.UserInvisibleModule)
	t.ModuleManager.RegisterModule(LabelAccessInterceptors, t.initLabelAccessInterceptors, modules.UserInvisibleModule)
	t.ModuleManager.RegisterModule(LabelAccessTripperware, t.initLabelAccessMiddleware, modules.UserInvisibleModule)
	t.ModuleManager.RegisterModule(Filterers, t.initFilterers, modules.UserInvisibleModule)
	t.ModuleManager.RegisterModule(LabelAccessUserIDTransformer, t.initLabelAccessUserIDTransformer, modules.UserInvisibleModule)
	t.ModuleManager.RegisterModule(AuthTripperware, t.initAuthTripperware, modules.UserInvisibleModule)

	lbacDeps := map[string][]string{
		AuthMiddleware:          {Server},
		AuthTripperware:         {QueryFrontendTripperware},
		Compactor:               {AuthMiddleware},
		Distributor:             {AuthMiddleware},
		IndexGateway:            {LabelAccess},
		Ingester:                {LabelAccessStoreWrapper, LabelAccessIngesterWrapper, Filterers},
		LabelAccess:             {Store},
		LabelAccessStoreWrapper: {Store},
		LabelAccessTripperware:  {AuthTripperware},
		Querier:                 {AuthMiddleware, LabelAccess, LabelAccessStoreWrapper},
		QueryEngine:             {AuthMiddleware, LabelAccessV2Engine},
		QueryFrontend:           {AuthMiddleware, LabelAccessTripperware},
		Server:                  {LabelAccessInterceptors, LabelAccessUserIDTransformer},
	}

	for mod, targets := range lbacDeps {
		if err := t.ModuleManager.AddDependency(mod, targets...); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loki) initAuthMiddleware() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing the auth middleware")

	l.HTTPAuthMiddleware = middleware.Merge(
		l.HTTPAuthMiddleware,
		labelaccess.NewLabelAccessMiddleware(
			log.With(util_log.Logger, "component", "label_access_middleware"),
		),
	)

	return nil, nil
}

func (l *Loki) initLabelAccessIngesterWrapper() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing ingester wrapper")
	l.Cfg.Ingester.Wrapper = &labelaccess.IngesterWrapper{}

	return nil, nil
}

func (l *Loki) initStoreChunkFilterer() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing store chunk filterer")
	filterer := labelaccess.RequestChunkFilterer{}
	l.Store.SetChunkFilterer(&filterer)
	return nil, nil
}

func (l *Loki) initLabelAccessInterceptors() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing label access interceptors")
	// Add server GRPC interceptors to the Ingester
	l.Cfg.Server.GRPCMiddleware = append(l.Cfg.Server.GRPCMiddleware, labelaccess.ServerLBACInterceptor)
	l.Cfg.Server.GRPCStreamMiddleware = append(l.Cfg.Server.GRPCStreamMiddleware, labelaccess.StreamServerLBACInterceptor)

	// Add client GRPC interceptors to the IngesterClient
	l.Cfg.IngesterClient.GRPCUnaryClientInterceptors = append(l.Cfg.IngesterClient.GRPCUnaryClientInterceptors, labelaccess.ClientLBACInterceptor)
	l.Cfg.IngesterClient.GRCPStreamClientInterceptors = append(l.Cfg.IngesterClient.GRCPStreamClientInterceptors, labelaccess.StreamClientLBACInterceptor)

	// Add client GRPC interceptors to the IndexGateway client
	l.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.GRPCUnaryClientInterceptors = append(l.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.GRPCUnaryClientInterceptors, labelaccess.ClientLBACInterceptor)
	l.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.GRCPStreamClientInterceptors = append(l.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.GRCPStreamClientInterceptors, labelaccess.StreamClientLBACInterceptor)

	return nil, nil
}

func (l *Loki) initLabelAccessMiddleware() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing label access middleware")
	l.QueryFrontEndMiddleware = queryrangebase.MergeMiddlewares(
		labelaccess.NewMiddleware(),
		l.QueryFrontEndMiddleware,
	)

	return nil, nil
}

func (l *Loki) initLabelAccessStoreWrapper() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing store wrapper")
	l.Store = labelaccess.WrapStore(l.Store)

	return nil, nil
}

func (l *Loki) initLabelAccessV2Engine() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing LBAC support for V2 query engine")

	// Set the StreamFilterer for filtering streams based on LBAC policies.
	// LBAC policies are propagated via HTTP headers using PropagateAllHeadersMiddleware
	// and httpreq.InjectHeader, then extracted at the worker via httpreq.ExtractAllHeaders.
	streamFilterer := &labelaccess.RequestStreamFilterer{}
	l.Cfg.QueryEngine.Executor.StreamFilterer = streamFilterer

	return nil, nil
}

func (l *Loki) initFilterers() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing label access filterers")
	filterer := labelaccess.RequestChunkFilterer{}
	l.Cfg.Ingester.ChunkFilterer = &filterer

	return nil, nil
}

func (l *Loki) initLabelAccessUserIDTransformer() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing label access id transformer")
	l.Cfg.QueryRange.Transformer = labelaccess.UserIDTransformer

	return nil, nil
}

func (l *Loki) initAuthTripperware() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing auth tripperware")
	return nil, nil
}
