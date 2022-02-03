package main

import (
	"flag"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/ViaQ/logerr/log"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/controllers"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/metrics"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	// +kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(lokiv1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr              string
		enableLeaderElection     bool
		probeAddr                string
		enableCertSigning        bool
		enableServiceMonitors    bool
		enableTLSServiceMonitors bool
		enableGateway            bool
		enableGatewayRoute       bool
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableCertSigning, "with-cert-signing-service", false,
		"Enables features in an Openshift cluster.")
	flag.BoolVar(&enableServiceMonitors, "with-service-monitors", false, "Enables service monitoring")
	flag.BoolVar(&enableTLSServiceMonitors, "with-tls-service-monitors", false,
		"Enables loading of a prometheus service monitor.")
	flag.BoolVar(&enableGateway, "with-lokistack-gateway", false,
		"Enables the manifest creation for the entire lokistack-gateway.")
	flag.BoolVar(&enableGatewayRoute, "with-lokistack-gateway-route", false,
		"Enables the usage of Route for the lokistack-gateway instead of Ingress (OCP Only!)")
	flag.Parse()

	log.Init("loki-operator")
	ctrl.SetLogger(log.GetLogger())

	if enableServiceMonitors || enableTLSServiceMonitors {
		utilruntime.Must(monitoringv1.AddToScheme(scheme))
	}

	if enableGateway {
		utilruntime.Must(configv1.AddToScheme(scheme))

		if enableGatewayRoute {
			utilruntime.Must(routev1.AddToScheme(scheme))
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "e3716011.grafana.com",
	})
	if err != nil {
		log.Error(err, "unable to start manager")
		os.Exit(1)
	}

	featureFlags := manifests.FeatureFlags{
		EnableCertificateSigningService: enableCertSigning,
		EnableServiceMonitors:           enableServiceMonitors,
		EnableTLSServiceMonitorConfig:   enableTLSServiceMonitors,
		EnableGateway:                   enableGateway,
		EnableGatewayRoute:              enableGatewayRoute,
	}

	if err = (&controllers.LokiStackReconciler{
		Client: mgr.GetClient(),
		Log:    log.WithName("controllers").WithName("LokiStack"),
		Scheme: mgr.GetScheme(),
		Flags:  featureFlags,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "LokiStack")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err = mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	log.Info("registering metrics")
	metrics.RegisterMetricCollectors()

	log.Info("Registering profiling endpoints.")
	err = registerProfiler(mgr)
	if err != nil {
		log.Error(err, "failed to register extra pprof handler")
		os.Exit(1)
	}

	log.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "problem running manager")
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
