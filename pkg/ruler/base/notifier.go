package base

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	gklog "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/dns"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/notifier"

	ruler_config "github.com/grafana/loki/v3/pkg/ruler/config"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// TODO: Instead of using the same metrics for all notifiers,
// should we have separate metrics for each discovery.NewManager?
var (
	sdMetrics map[string]discovery.DiscovererMetrics

	srvDNSregexp = regexp.MustCompile(`^_.+._.+`)
)

func init() {
	var err error
	sdMetrics, err = discovery.CreateAndRegisterSDMetrics(prometheus.DefaultRegisterer)
	if err != nil {
		panic(err)
	}
}

// rulerNotifier bundles a notifier.Manager together with an associated
// Alertmanager service discovery manager and handles the lifecycle
// of both actors.
type rulerNotifier struct {
	notifier  *notifier.Manager
	sdCancel  context.CancelFunc
	sdManager *discovery.Manager
	wg        sync.WaitGroup
	logger    gklog.Logger
}

func newRulerNotifier(o *notifier.Options, l gklog.Logger) *rulerNotifier {
	sdCtx, sdCancel := context.WithCancel(context.Background())
	return &rulerNotifier{
		notifier:  notifier.NewManager(o, util_log.SlogFromGoKit(l)),
		sdCancel:  sdCancel,
		sdManager: discovery.NewManager(sdCtx, util_log.SlogFromGoKit(l), util.NoopRegistry{}, sdMetrics),
		logger:    l,
	}
}

// run starts the notifier. This function doesn't block and returns immediately.
func (rn *rulerNotifier) run() {
	rn.wg.Add(2)
	go func() {
		if err := rn.sdManager.Run(); err != nil {
			level.Error(rn.logger).Log("msg", "error starting notifier discovery manager", "err", err)
		}
		rn.wg.Done()
	}()
	go func() {
		rn.notifier.Run(rn.sdManager.SyncCh())
		rn.wg.Done()
	}()
}

func (rn *rulerNotifier) applyConfig(cfg *config.Config) error {
	if err := rn.notifier.ApplyConfig(cfg); err != nil {
		return err
	}

	sdCfgs := make(map[string]discovery.Configs)
	for k, v := range cfg.AlertingConfig.AlertmanagerConfigs.ToMap() {
		sdCfgs[k] = v.ServiceDiscoveryConfigs
	}
	return rn.sdManager.ApplyConfig(sdCfgs)
}

func (rn *rulerNotifier) stop() {
	rn.sdCancel()
	rn.notifier.Stop()
	rn.wg.Wait()
}

func applyAlertmanagerDefaults(config ruler_config.AlertManagerConfig) ruler_config.AlertManagerConfig {
	// Use default value if the override values are zero
	if config.AlertmanagerRefreshInterval == 0 {
		config.AlertmanagerRefreshInterval = alertmanagerRefreshIntervalDefault
	}

	if config.NotificationQueueCapacity <= 0 {
		config.NotificationQueueCapacity = alertmanagerNotificationQueueCapacityDefault
	}

	if config.NotificationTimeout == 0 {
		config.NotificationTimeout = alertmanagerNotificationTimeoutDefault
	}

	return config
}

// Builds a Prometheus config.Config from a ruler.Config with just the required
// options to configure notifications to Alertmanager.
func buildNotifierConfig(amConfig *ruler_config.AlertManagerConfig, externalLabels labels.Labels) (*config.Config, error) {
	amURLs := strings.Split(amConfig.AlertmanagerURL, ",")
	validURLs := make([]*url.URL, 0, len(amURLs))

	for _, h := range amURLs {
		url, err := url.Parse(h)
		if err != nil {
			return nil, err
		}

		if url.String() == "" {
			continue
		}

		// Given we only support SRV lookups as part of service discovery, we need to ensure
		// hosts provided follow this specification: _service._proto.name
		// e.g. _http._tcp.alertmanager.com
		if amConfig.AlertmanagerDiscovery && !srvDNSregexp.MatchString(url.Host) {
			return nil, fmt.Errorf("when alertmanager-discovery is on, host name must be of the form _portname._tcp.service.fqdn (is %q)", url.Host)
		}

		validURLs = append(validURLs, url)
	}

	if len(validURLs) == 0 {
		return &config.Config{}, nil
	}

	apiVersion := config.AlertmanagerAPIVersionV1
	if amConfig.AlertmanangerEnableV2API {
		apiVersion = config.AlertmanagerAPIVersionV2
	}

	amConfigs := make([]*config.AlertmanagerConfig, 0, len(validURLs))
	for _, url := range validURLs {
		amConfigs = append(amConfigs, amConfigFromURL(amConfig, url, apiVersion))
	}

	promConfig := &config.Config{
		GlobalConfig: config.GlobalConfig{
			ExternalLabels: externalLabels,
		},
		AlertingConfig: config.AlertingConfig{
			AlertRelabelConfigs: amConfig.AlertRelabelConfigs,
			AlertmanagerConfigs: amConfigs,
		},
	}

	return promConfig, nil
}

func amConfigFromURL(cfg *ruler_config.AlertManagerConfig, url *url.URL, apiVersion config.AlertmanagerAPIVersion) *config.AlertmanagerConfig {
	var sdConfig discovery.Configs
	if cfg.AlertmanagerDiscovery {
		sdConfig = discovery.Configs{
			&dns.SDConfig{
				Names:           []string{url.Host},
				RefreshInterval: model.Duration(cfg.AlertmanagerRefreshInterval),
				Type:            "SRV",
				Port:            0, // Ignored, because of SRV.
			},
		}

	} else {
		sdConfig = discovery.Configs{
			discovery.StaticConfig{
				{
					Targets: []model.LabelSet{{model.AddressLabel: model.LabelValue(url.Host)}},
				},
			},
		}
	}

	amConfig := &config.AlertmanagerConfig{
		APIVersion:              apiVersion,
		Scheme:                  url.Scheme,
		PathPrefix:              url.Path,
		Timeout:                 model.Duration(cfg.NotificationTimeout),
		ServiceDiscoveryConfigs: sdConfig,
		HTTPClientConfig: config_util.HTTPClientConfig{
			TLSConfig: config_util.TLSConfig{
				CAFile:             cfg.Notifier.TLS.CAPath,
				CertFile:           cfg.Notifier.TLS.CertPath,
				KeyFile:            cfg.Notifier.TLS.KeyPath,
				InsecureSkipVerify: cfg.Notifier.TLS.InsecureSkipVerify,
				ServerName:         cfg.Notifier.TLS.ServerName,
			},
		},
	}

	// Check the URL for basic authentication information first
	if url.User != nil {
		amConfig.HTTPClientConfig.BasicAuth = &config_util.BasicAuth{
			Username: url.User.Username(),
		}

		if password, isSet := url.User.Password(); isSet {
			amConfig.HTTPClientConfig.BasicAuth.Password = config_util.Secret(password)
		}
	}

	// Override URL basic authentication configs with hard coded config values if present
	if cfg.Notifier.BasicAuth.IsEnabled() {
		amConfig.HTTPClientConfig.BasicAuth = &config_util.BasicAuth{
			Username: cfg.Notifier.BasicAuth.Username,
			Password: config_util.Secret(cfg.Notifier.BasicAuth.Password),
		}
	}

	if cfg.Notifier.HeaderAuth.IsEnabled() {
		if cfg.Notifier.HeaderAuth.Credentials != "" {
			amConfig.HTTPClientConfig.Authorization = &config_util.Authorization{
				Type:        cfg.Notifier.HeaderAuth.Type,
				Credentials: config_util.Secret(cfg.Notifier.HeaderAuth.Credentials),
			}
		} else if cfg.Notifier.HeaderAuth.CredentialsFile != "" {
			amConfig.HTTPClientConfig.Authorization = &config_util.Authorization{
				Type:            cfg.Notifier.HeaderAuth.Type,
				CredentialsFile: cfg.Notifier.HeaderAuth.CredentialsFile,
			}

		}
	}

	return amConfig
}
