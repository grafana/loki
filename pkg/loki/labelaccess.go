package loki

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v4/pkg/labelaccess"

	"github.com/grafana/loki/v4/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/v4/pkg/util/log"
)

func (l *Loki) setupLBAC() error {
	l.Codec = labelaccess.NewCodec(l.Codec)

	l.ModuleManager.RegisterModule(AuthMiddleware, l.initAuthMiddleware, modules.UserInvisibleModule)
	l.ModuleManager.RegisterModule(LabelAccess, l.initStoreChunkFilterer)
	l.ModuleManager.RegisterModule(LabelAccessStoreWrapper, l.initLabelAccessStoreWrapper, modules.UserInvisibleModule)
	l.ModuleManager.RegisterModule(LabelAccessIngesterWrapper, l.initLabelAccessIngesterWrapper)
	l.ModuleManager.RegisterModule(LabelAccessInterceptors, l.initLabelAccessInterceptors, modules.UserInvisibleModule)
	l.ModuleManager.RegisterModule(LabelAccessTripperware, l.initLabelAccessMiddleware, modules.UserInvisibleModule)
	l.ModuleManager.RegisterModule(Filterers, l.initFilterers, modules.UserInvisibleModule)
	l.ModuleManager.RegisterModule(LabelAccessUserIDTransformer, l.initLabelAccessUserIDTransformer, modules.UserInvisibleModule)
	l.ModuleManager.RegisterModule(AuthTripperware, l.initAuthTripperware, modules.UserInvisibleModule)

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
		QueryFrontend:           {AuthMiddleware, LabelAccessTripperware},
		Server:                  {LabelAccessInterceptors, LabelAccessUserIDTransformer},
	}

	for mod, targets := range lbacDeps {
		if err := l.ModuleManager.AddDependency(mod, targets...); err != nil {
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
	l.lbacChunkFilterer = &filterer
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
