package manifests

import (
	"github.com/ViaQ/logerr/kverrors"
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests/internal/gateway"
	"github.com/ViaQ/loki-operator/internal/manifests/openshift"
	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyGatewayDefaultOptions applies defaults on the LokiStackSpec depending on selected
// tenant mode. Currently nothing is applied for modes static and dynamic. For mode openshift-logging
// the tenant spec is filled with defaults for authentication and authorization.
func ApplyGatewayDefaultOptions(opts *Options) error {
	switch opts.Stack.Tenants.Mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // continue using user input

	case lokiv1beta1.OpenshiftLogging:
		defaults, err := openshift.NewOptions(
			opts.Name,
			GatewayName(opts.Name),
			opts.Namespace,
			opts.GatewayHost,
			serviceNameGatewayHTTP(opts.Name),
			gatewayHTTPPortName,
			ComponentLabels(LabelGatewayComponent, opts.Name),
		)
		if err != nil {
			return kverrors.Wrap(err, "failed to create new openshift options")
		}

		if err := mergo.Merge(&opts.OpenShiftOptions, &defaults, mergo.WithOverride); err != nil {
			return kverrors.Wrap(err, "failed to merge defaults for mode openshift logging")
		}
	}

	return nil
}

func configureDeploymentForMode(d *appsv1.DeploymentSpec, mode lokiv1beta1.ModeType, flags FeatureFlags) error {
	switch mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // nothing to configure
	case lokiv1beta1.OpenshiftLogging:
		return openshift.ConfigureDeployment(
			d,
			tlsMetricsSercetVolume,
			gateway.LokiGatewayTLSDir,
			gateway.LokiGatewayCertFile,
			gateway.LokiGatewayKeyFile,
			flags.EnableTLSServiceMonitorConfig,
		)
	}

	return nil
}

func configureServiceForMode(s *corev1.ServiceSpec, mode lokiv1beta1.ModeType) error {
	switch mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // nothing to configure
	case lokiv1beta1.OpenshiftLogging:
		return openshift.ConfigureService(s)
	}

	return nil
}

func configureGatewayObjsForMode(objs []client.Object, opts Options) []client.Object {
	switch opts.Stack.Tenants.Mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		// nothing to configure
	case lokiv1beta1.OpenshiftLogging:
		openShiftObjs := openshift.Build(opts.OpenShiftOptions)

		var cObjs []client.Object
		for _, o := range objs {
			switch o.(type) {
			// Drop Ingress in favor of Route in OpenShift.
			// Ingress is not supported as OAuthRedirectReference
			// in ServiceAccounts used as OAuthClient in OpenShift.
			case *networkingv1.Ingress:
				continue
			}

			cObjs = append(cObjs, o)
		}

		objs = append(cObjs, openShiftObjs...)
	}

	return objs
}

func configureServiceMonitorForMode(sm *monitoringv1.ServiceMonitor, mode lokiv1beta1.ModeType, flags FeatureFlags) error {
	switch mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // nothing to configure
	case lokiv1beta1.OpenshiftLogging:
		return openshift.ConfigureServiceMonitor(sm, flags.EnableTLSServiceMonitorConfig)
	}

	return nil
}
