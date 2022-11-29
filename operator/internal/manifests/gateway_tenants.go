package manifests

import (
	"github.com/ViaQ/logerr/v2/kverrors"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyGatewayDefaultOptions applies defaults on the LokiStackSpec depending on selected
// tenant mode. Currently nothing is applied for modes static and dynamic.
// For modes openshift-logging and openshift-network
// the tenant spec is filled with defaults for authentication and authorization.
func ApplyGatewayDefaultOptions(opts *Options) error {
	if opts.Stack.Tenants == nil {
		return nil
	}

	switch opts.Stack.Tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		return nil // continue using user input

	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		tenantData := make(map[string]openshift.TenantData)
		for name, tenant := range opts.Tenants.Configs {
			tenantData[name] = openshift.TenantData{
				CookieSecret: tenant.OpenShift.CookieSecret,
			}
		}

		defaults := openshift.NewOptions(
			opts.Stack.Tenants.Mode,
			opts.Name,
			opts.Namespace,
			GatewayName(opts.Name),
			opts.GatewayBaseDomain,
			serviceNameGatewayHTTP(opts.Name),
			gatewayHTTPPortName,
			ComponentLabels(LabelGatewayComponent, opts.Name),
			tenantData,
			RulerName(opts.Name),
		)

		if err := mergo.Merge(&opts.OpenShiftOptions, &defaults, mergo.WithOverride); err != nil {
			return kverrors.Wrap(err, "failed to merge defaults for mode openshift")
		}

	}

	return nil
}

func configureGatewayDeploymentForMode(d *appsv1.Deployment, mode lokiv1.ModeType, fg configv1.FeatureGates, minTLSVersion string, ciphers string) error {
	switch mode {
	case lokiv1.Static, lokiv1.Dynamic:
		return nil // nothing to configure
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		tlsDir := gatewayServerHTTPTLSDir()
		return openshift.ConfigureGatewayDeployment(d, mode, tlsSecretVolume, tlsDir, minTLSVersion, ciphers, fg.HTTPEncryption)
	}

	return nil
}

func configureGatewayServiceForMode(s *corev1.ServiceSpec, mode lokiv1.ModeType) error {
	switch mode {
	case lokiv1.Static, lokiv1.Dynamic:
		return nil // nothing to configure
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		return openshift.ConfigureGatewayService(s)
	}

	return nil
}

func configureGatewayObjsForMode(objs []client.Object, opts Options) []client.Object {
	switch opts.Stack.Tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		// nothing to configure
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		for _, o := range objs {
			switch sa := o.(type) {
			case *corev1.ServiceAccount:
				if sa.Annotations == nil {
					sa.Annotations = map[string]string{}
				}

				a := openshift.ServiceAccountAnnotations(opts.OpenShiftOptions)
				for key, value := range a {
					sa.Annotations[key] = value
				}
			}
		}

		openShiftObjs := openshift.BuildGatewayObjects(opts.OpenShiftOptions)

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

func configureGatewayServiceMonitorForMode(sm *monitoringv1.ServiceMonitor, opts Options) error {
	switch opts.Stack.Tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		return nil // nothing to configure
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		return openshift.ConfigureGatewayServiceMonitor(sm, opts.Gates.ServiceMonitorTLSEndpoints)
	}

	return nil
}

// ConfigureOptionsForMode applies configuration depending on the mode type.
func ConfigureOptionsForMode(cfg *config.Options, opt Options) error {
	switch opt.Stack.Tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		return nil // nothing to configure
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		if opt.OpenShiftOptions.BuildOpts.AlertManagerEnabled {
			return openshift.ConfigureOptions(cfg)
		}
		return nil
	}

	return nil
}
