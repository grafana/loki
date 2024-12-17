package manifests

import (
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// ApplyGatewayDefaultOptions applies defaults on the LokiStackSpec depending on selected
// tenant mode. Currently nothing is applied for modes static and dynamic.
// For modes openshift-logging and openshift-network
// the tenant spec is filled with defaults for authentication and authorization.
func ApplyGatewayDefaultOptions(opts *Options) error {
	if opts.Stack.Tenants == nil {
		return nil
	}

	if !opts.Gates.OpenShift.Enabled {
		return nil
	}

	o := openshift.NewOptions(
		opts.Name,
		opts.Namespace,
		GatewayName(opts.Name),
		serviceNameGatewayHTTP(opts.Name),
		gatewayHTTPPortName,
		opts.Timeouts.Gateway.WriteTimeout,
		ComponentLabels(LabelGatewayComponent, opts.Name),
		RulerName(opts.Name),
	)

	switch opts.Stack.Tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		// Do nothing as per tenants provided by LokiStack CR
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		tenantData := make(map[string]openshift.TenantData)
		for name, tenant := range opts.Tenants.Configs {
			tenantData[name] = openshift.TenantData{
				CookieSecret: tenant.OpenShift.CookieSecret,
			}
		}

		o.WithTenantsForMode(opts.Stack.Tenants.Mode, opts.GatewayBaseDomain, tenantData)
	}

	if err := mergo.Merge(&opts.OpenShiftOptions, o, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge defaults for mode openshift")
	}

	return nil
}

func configureGatewayDeploymentForMode(d *appsv1.Deployment, tenants *lokiv1.TenantsSpec, fg configv1.FeatureGates, minTLSVersion string, ciphers string, adminGroups []string) error {
	switch tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		if tenants != nil {
			return configureCAVolumes(d, tenants)
		}
		return nil
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		tlsDir := gatewayServerHTTPTLSDir()
		return openshift.ConfigureGatewayDeployment(d, tenants.Mode, tlsSecretVolume, tlsDir, minTLSVersion, ciphers, fg.HTTPEncryption, adminGroups)
	}

	return nil
}

func configureGatewayDeploymentRulesAPIForMode(d *appsv1.Deployment, mode lokiv1.ModeType) error {
	switch mode {
	case lokiv1.Static, lokiv1.Dynamic, lokiv1.OpenshiftNetwork:
		return nil // nothing to configure
	case lokiv1.OpenshiftLogging:
		return openshift.ConfigureGatewayDeploymentRulesAPI(d, gatewayContainerName)
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
	if !opts.Gates.OpenShift.Enabled {
		return objs
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

	switch opts.Stack.Tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		// If a single tenant configure mTLS change Route termination policy
		// to Passthrough
		for _, o := range objs {
			switch r := o.(type) {
			case *routev1.Route:
				for _, secret := range opts.Tenants.Secrets {
					if secret.MTLSSecret != nil {
						r.Spec.TLS.Termination = routev1.TLSTerminationPassthrough
						break
					}
				}
			}
		}
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

		openShiftObjs := openshift.BuildGatewayTenantModeObjects(opts.OpenShiftOptions)
		objs = append(objs, openShiftObjs...)
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
	case lokiv1.OpenshiftNetwork:
		return openshift.ConfigureOptions(cfg, opt.OpenShiftOptions.BuildOpts.AlertManagerEnabled, false, "", "", "")
	case lokiv1.OpenshiftLogging:
		monitorServerName := fqdn(openshift.MonitoringSVCUserWorkload, openshift.MonitoringUserWorkloadNS)
		return openshift.ConfigureOptions(
			cfg,
			opt.OpenShiftOptions.BuildOpts.AlertManagerEnabled,
			opt.OpenShiftOptions.BuildOpts.UserWorkloadAlertManagerEnabled,
			BearerTokenFile,
			alertmanagerUpstreamCAPath(),
			monitorServerName,
		)
	}

	return nil
}

// configureCAVolumes will mount CA bundles for both OIDC and mTLS. Furthermore
// if a user configures mTLS it will also update the arg --tls.client-auth-type
func configureCAVolumes(d *appsv1.Deployment, tenants *lokiv1.TenantsSpec) error {
	if tenants.Authentication == nil {
		return nil // nothing to do
	}

	mountCAConfigMap := func(container *corev1.Container, volumes *[]corev1.Volume, tennantName, configmapName string) {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      tenantCAVolumeName(tennantName),
			MountPath: tenantCADir(tennantName),
		})
		*volumes = append(*volumes, corev1.Volume{
			Name: tenantCAVolumeName(tennantName),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configmapName,
					},
				},
			},
		})
	}

	var gwIndex int
	for i, c := range d.Spec.Template.Spec.Containers {
		if c.Name == gatewayContainerName {
			gwIndex = i
			break
		}
	}

	gwContainer := d.Spec.Template.Spec.Containers[gwIndex].DeepCopy()
	gwArgs := gwContainer.Args
	gwVolumes := d.Spec.Template.Spec.Volumes

	mTLS := false
	for _, tenant := range tenants.Authentication {
		switch {
		case tenant.OIDC != nil:
			if tenant.OIDC.IssuerCA != nil {
				mountCAConfigMap(gwContainer, &gwVolumes, tenant.TenantName, tenant.OIDC.IssuerCA.CA)
			}
		case tenant.MTLS != nil:
			mountCAConfigMap(gwContainer, &gwVolumes, tenant.TenantName, tenant.MTLS.CA.CA)
			mTLS = true
		}
	}

	if mTLS {
		// Remove old tls.client-auth-type
		for i, arg := range gwArgs {
			if strings.HasPrefix(arg, "--tls.client-auth-type=") {
				gwArgs = append(gwArgs[:i], gwArgs[i+1:]...)
				break
			}
		}
		gwArgs = append(gwArgs, "--tls.client-auth-type=RequestClientCert")
		gwContainer.Args = gwArgs
	}

	p := corev1.PodSpec{
		Containers: []corev1.Container{
			*gwContainer,
		},
		Volumes: gwVolumes,
	}
	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge server pki into container spec ")
	}
	return nil
}
