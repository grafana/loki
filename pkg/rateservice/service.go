package rateservice

import (
	"context"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
)

type Service struct {
	services.Service
	cfg    Config
	store  *rateStore
	logger log.Logger
	reg    prometheus.Registerer
}

func NewService(cfg Config, reg prometheus.Registerer, logger log.Logger) *Service {
	s := Service{
		cfg:    cfg,
		store:  newRateStore(cfg.WindowSecs, cfg.BucketSizeSecs),
		logger: logger,
		reg:    reg,
	}
	reg.MustRegister(s.store)
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return &s
}

// starting implements [services.StartingFn].
func (s *Service) starting(ctx context.Context) error {
	return nil
}

// running implements [services.RunningFn].
func (s *Service) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// stopping implements [services.StoppingFn].
func (s *Service) stopping(failureCase error) error {
	return nil
}

// AllRealms implements the [proto.RateServiceServer] interface.
func (s *Service) AllRealms(
	_ *proto.AllRealmsRequest,
	conn proto.RateService_AllRealmsServer,
) error {
	realms := s.store.AllRealms()
	for _, realm := range realms {
		if err := conn.Send(realm); err != nil {
			return err
		}
	}
	return nil
}

// GetRates implements the [proto.RateServiceServer] interface.
func (s *Service) GetRealm(
	_ context.Context,
	req *proto.GetRealmRequest,
) (*proto.GetRealmResponse, error) {
	rates, _ := s.store.GetRealm(req.Realm)
	return &proto.GetRealmResponse{
		Realm:   req.Realm,
		Results: rates,
	}, nil
}

// UpdateRealm implements the [proto.RateServiceServer] interface.
func (s *Service) UpdateRealm(
	_ context.Context,
	req *proto.UpdateRealmRequest,
) (*proto.UpdateRealmResponse, error) {
	s.store.UpdateRealm(req.Realm, req.Params)
	return &proto.UpdateRealmResponse{}, nil
}
