package frontend

import (
	"flag"
	"net/http"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/frontend/transport"
	v1 "github.com/cortexproject/cortex/pkg/frontend/v1"
	v2 "github.com/cortexproject/cortex/pkg/frontend/v2"
	"github.com/cortexproject/cortex/pkg/util"
)

// This struct combines several configuration options together to preserve backwards compatibility.
type CombinedFrontendConfig struct {
	Handler    transport.HandlerConfig `yaml:",inline"`
	FrontendV1 v1.Config               `yaml:",inline"`
	FrontendV2 v2.Config               `yaml:",inline"`

	DownstreamURL string `yaml:"downstream_url"`
}

func (cfg *CombinedFrontendConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.Handler.RegisterFlags(f)
	cfg.FrontendV1.RegisterFlags(f)
	cfg.FrontendV2.RegisterFlags(f)

	f.StringVar(&cfg.DownstreamURL, "frontend.downstream-url", "", "URL of downstream Prometheus.")
}

// InitFrontend initializes frontend (either V1 -- without scheduler, or V2 -- with scheduler) or no frontend at
// all if downstream Prometheus URL is used instead.
//
// Returned RoundTripper can be wrapped in more round-tripper middlewares, and then eventually registered
// into HTTP server using the Handler from this package. Returned RoundTripper is always non-nil
// (if there are no errors), and it uses the returned frontend (if any).
func InitFrontend(cfg CombinedFrontendConfig, limits v1.Limits, grpcListenPort int, log log.Logger, reg prometheus.Registerer) (http.RoundTripper, *v1.Frontend, *v2.Frontend, error) {
	switch {
	case cfg.DownstreamURL != "":
		// If the user has specified a downstream Prometheus, then we should use that.
		rt, err := NewDownstreamRoundTripper(cfg.DownstreamURL)
		return rt, nil, nil, err

	case cfg.FrontendV2.SchedulerAddress != "":
		// If query-scheduler address is configured, use Frontend.
		if cfg.FrontendV2.Addr == "" {
			addr, err := util.GetFirstAddressOf(cfg.FrontendV2.InfNames)
			if err != nil {
				return nil, nil, nil, errors.Wrap(err, "failed to get frontend address")
			}

			cfg.FrontendV2.Addr = addr
		}

		if cfg.FrontendV2.Port == 0 {
			cfg.FrontendV2.Port = grpcListenPort
		}

		fr, err := v2.NewFrontend(cfg.FrontendV2, log, reg)
		return transport.AdaptGrpcRoundTripperToHTTPRoundTripper(fr), nil, fr, err

	default:
		// No scheduler = use original frontend.
		fr, err := v1.New(cfg.FrontendV1, limits, log, reg)
		if err != nil {
			return nil, nil, nil, err
		}
		return transport.AdaptGrpcRoundTripperToHTTPRoundTripper(fr), fr, nil, nil
	}
}
