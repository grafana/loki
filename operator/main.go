package main

import (
	"flag"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/ViaQ/logerr/v2/log"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	ctrlconfigv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	lokictrl "github.com/grafana/loki/operator/controllers/loki"
	"github.com/grafana/loki/operator/internal/metrics"
	"github.com/grafana/loki/operator/internal/validation"
	"github.com/grafana/loki/operator/internal/validation/openshift"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(lokiv1beta1.AddToScheme(scheme))

	utilruntime.Must(lokiv1.AddToScheme(scheme))

	utilruntime.Must(ctrlconfigv1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "",
		"The controller will load its initial configuration from this file. "+
			"Omit this flag to use the default configuration values. "+
			"Command-line flags override configuration from this file.",
	)
	flag.Parse()

	logger := log.NewLogger("loki-operator")
	ctrl.SetLogger(logger)

	var err error

	ctrlCfg := ctrlconfigv1.ProjectConfig{}
	options := ctrl.Options{Scheme: scheme}
	if configFile != "" {
		options, err = options.AndFrom(ctrl.ConfigFile().AtPath(configFile).OfKind(&ctrlCfg))
		if err != nil {
			logger.Error(err, "failed to parse controller manager config file")
			os.Exit(1)
		}
	}

	if ctrlCfg.Gates.LokiStackAlerts && !ctrlCfg.Gates.ServiceMonitors {
		logger.Error(kverrors.New("LokiStackAlerts flag requires ServiceMonitors"), "")
		os.Exit(1)
	}

	if ctrlCfg.Gates.ServiceMonitorTLSEndpoints && !ctrlCfg.Gates.HTTPEncryption {
		logger.Error(kverrors.New("ServiceMonitorTLSEndpoints flag requires HTTPEncryption"), "")
		os.Exit(1)
	}

	if ctrlCfg.Gates.ServiceMonitors || ctrlCfg.Gates.ServiceMonitorTLSEndpoints {
		utilruntime.Must(monitoringv1.AddToScheme(scheme))
	}

	if ctrlCfg.Gates.LokiStackGateway {
		utilruntime.Must(configv1.AddToScheme(scheme))

		if ctrlCfg.Gates.OpenShift.Enabled {
			utilruntime.Must(routev1.AddToScheme(scheme))
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&lokictrl.LokiStackReconciler{
		Client:       mgr.GetClient(),
		Log:          logger.WithName("controllers").WithName("lokistack"),
		Scheme:       mgr.GetScheme(),
		FeatureGates: ctrlCfg.Gates,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "lokistack")
		os.Exit(1)
	}
	if ctrlCfg.Gates.LokiStackWebhook {
		v := &validation.LokiStackValidator{}
		if err = v.SetupWebhookWithManager(mgr); err != nil {
			logger.Error(err, "unable to create webhook", "webhook", "lokistack")
			os.Exit(1)
		}
	}
	if err = (&lokictrl.AlertingRuleReconciler{
		Client: mgr.GetClient(),
		Log:    logger.WithName("controllers").WithName("alertingrule"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "alertingrule")
		os.Exit(1)
	}
	if ctrlCfg.Gates.AlertingRuleWebhook {
		v := &validation.AlertingRuleValidator{}
		if ctrlCfg.Gates.OpenShift.ExtendedRuleValidation {
			v.ExtendedValidator = openshift.AlertingRuleValidator
		}

		if err = v.SetupWebhookWithManager(mgr); err != nil {
			logger.Error(err, "unable to create webhook", "webhook", "alertingrule")
			os.Exit(1)
		}
	}
	if err = (&lokictrl.RecordingRuleReconciler{
		Client: mgr.GetClient(),
		Log:    logger.WithName("controllers").WithName("recordingrule"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "recordingrule")
		os.Exit(1)
	}
	if ctrlCfg.Gates.RecordingRuleWebhook {
		v := &validation.RecordingRuleValidator{}
		if ctrlCfg.Gates.OpenShift.ExtendedRuleValidation {
			v.ExtendedValidator = openshift.RecordingRuleValidator
		}

		if err = v.SetupWebhookWithManager(mgr); err != nil {
			logger.Error(err, "unable to create webhook", "webhook", "recordingrule")
			os.Exit(1)
		}
	}
	if err = (&lokictrl.RulerConfigReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "rulerconfig")
		os.Exit(1)
	}
	if ctrlCfg.Gates.RulerConfigWebhook {
		v := &validation.RulerConfigValidator{}
		if err = v.SetupWebhookWithManager(mgr); err != nil {
			logger.Error(err, "unable to create webhook", "webhook", "rulerconfig")
			os.Exit(1)
		}
	}
	if ctrlCfg.Gates.BuiltInCertManagement.Enabled {
		if err = (&lokictrl.CertRotationReconciler{
			Client:       mgr.GetClient(),
			Log:          logger.WithName("controllers").WithName("certrotation"),
			Scheme:       mgr.GetScheme(),
			FeatureGates: ctrlCfg.Gates,
		}).SetupWithManager(mgr); err != nil {
			logger.Error(err, "unable to create controller", "controller", "certrotation")
			os.Exit(1)
		}
	}

	if err = (&lokictrl.LokiStackZoneAwarePodReconciler{
		Client: mgr.GetClient(),
		Log:    logger.WithName("controllers").WithName("lokistack-zoneaware-pod"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "lokistack-zoneaware-pod")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err = mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	logger.Info("registering metrics")
	metrics.RegisterMetricCollectors()

	logger.Info("Registering profiling endpoints.")
	err = registerProfiler(mgr)
	if err != nil {
		logger.Error(err, "failed to register extra pprof handler")
		os.Exit(1)
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func registerProfiler(m ctrl.Manager) error {
	endpoints := map[string]http.HandlerFunc{
		"/debug/pprof/":        pprof.Index,
		"/debug/pprof/cmdline": pprof.Cmdline,
		"/debug/pprof/profile": pprof.Profile,
		"/debug/pprof/symbol":  pprof.Symbol,
		"/debug/pprof/trace":   pprof.Trace,
	}

	for path, handler := range endpoints {
		err := m.AddMetricsExtraHandler(path, handler)
		if err != nil {
			return err
		}
	}

	return nil
}
