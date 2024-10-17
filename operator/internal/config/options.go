package config

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
)

// LoadConfig initializes the controller configuration, optionally overriding the defaults
// from a provided configuration file.
func LoadConfig(scheme *runtime.Scheme, configFile string) (*configv1.ProjectConfig, *TokenCCOAuthConfig, ctrl.Options, error) {
	options := ctrl.Options{Scheme: scheme}
	if configFile == "" {
		return &configv1.ProjectConfig{}, nil, options, nil
	}

	ctrlCfg, err := loadConfigFile(scheme, configFile)
	if err != nil {
		return nil, nil, options, fmt.Errorf("failed to parse controller manager config file: %w", err)
	}

	tokenCCOAuth := discoverTokenCCOAuthConfig()
	if ctrlCfg.Gates.OpenShift.Enabled && tokenCCOAuth != nil {
		ctrlCfg.Gates.OpenShift.TokenCCOAuthEnv = true
	}

	options = mergeOptionsFromFile(options, ctrlCfg)
	return ctrlCfg, tokenCCOAuth, options, nil
}

func mergeOptionsFromFile(o manager.Options, cfg *configv1.ProjectConfig) manager.Options {
	o = setLeaderElectionConfig(o, cfg.ControllerManagerConfigurationSpec)

	if o.Metrics.BindAddress == "" && cfg.Metrics.BindAddress != "" {
		o.Metrics.BindAddress = cfg.Metrics.BindAddress

		endpoints := map[string]http.HandlerFunc{
			"/debug/pprof/":        pprof.Index,
			"/debug/pprof/cmdline": pprof.Cmdline,
			"/debug/pprof/profile": pprof.Profile,
			"/debug/pprof/symbol":  pprof.Symbol,
			"/debug/pprof/trace":   pprof.Trace,
		}

		if o.Metrics.ExtraHandlers == nil {
			o.Metrics.ExtraHandlers = map[string]http.Handler{}
		}

		for path, handler := range endpoints {
			o.Metrics.ExtraHandlers[path] = handler
		}
	}

	if o.HealthProbeBindAddress == "" && cfg.Health.HealthProbeBindAddress != "" {
		o.HealthProbeBindAddress = cfg.Health.HealthProbeBindAddress
	}

	if cfg.Webhook.Port != nil {
		o.WebhookServer = webhook.NewServer(webhook.Options{
			Port: *cfg.Webhook.Port,
		})
	}

	return o
}

func setLeaderElectionConfig(o manager.Options, obj configv1.ControllerManagerConfigurationSpec) manager.Options {
	if obj.LeaderElection == nil {
		// The source does not have any configuration; noop
		return o
	}

	if !o.LeaderElection && obj.LeaderElection.LeaderElect != nil {
		o.LeaderElection = *obj.LeaderElection.LeaderElect
	}

	if o.LeaderElectionResourceLock == "" && obj.LeaderElection.ResourceLock != "" {
		o.LeaderElectionResourceLock = obj.LeaderElection.ResourceLock
	}

	if o.LeaderElectionNamespace == "" && obj.LeaderElection.ResourceNamespace != "" {
		o.LeaderElectionNamespace = obj.LeaderElection.ResourceNamespace
	}

	if o.LeaderElectionID == "" && obj.LeaderElection.ResourceName != "" {
		o.LeaderElectionID = obj.LeaderElection.ResourceName
	}

	if o.LeaseDuration == nil && !reflect.DeepEqual(obj.LeaderElection.LeaseDuration, metav1.Duration{}) {
		o.LeaseDuration = &obj.LeaderElection.LeaseDuration.Duration
	}

	if o.RenewDeadline == nil && !reflect.DeepEqual(obj.LeaderElection.RenewDeadline, metav1.Duration{}) {
		o.RenewDeadline = &obj.LeaderElection.RenewDeadline.Duration
	}

	if o.RetryPeriod == nil && !reflect.DeepEqual(obj.LeaderElection.RetryPeriod, metav1.Duration{}) {
		o.RetryPeriod = &obj.LeaderElection.RetryPeriod.Duration
	}

	return o
}
