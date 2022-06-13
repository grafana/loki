package manifests

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

// BuildServiceMonitors builds the service monitors
func BuildServiceMonitors(opts Options) []client.Object {
	return []client.Object{
		NewDistributorServiceMonitor(opts),
		NewIngesterServiceMonitor(opts),
		NewQuerierServiceMonitor(opts),
		NewCompactorServiceMonitor(opts),
		NewQueryFrontendServiceMonitor(opts),
		NewIndexGatewayServiceMonitor(opts),
		NewRulerServiceMonitor(opts),
		NewGatewayServiceMonitor(opts),
	}
}

// NewDistributorServiceMonitor creates a k8s service monitor for the distributor component
func NewDistributorServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelDistributorComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(DistributorName(opts.Name))
	serviceName := serviceNameDistributorHTTP(opts.Name)
	lokiEndpoint := serviceMonitorEndpoint(lokiHTTPPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewIngesterServiceMonitor creates a k8s service monitor for the ingester component
func NewIngesterServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelIngesterComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(IngesterName(opts.Name))
	serviceName := serviceNameIngesterHTTP(opts.Name)
	lokiEndpoint := serviceMonitorEndpoint(lokiHTTPPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewQuerierServiceMonitor creates a k8s service monitor for the querier component
func NewQuerierServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelQuerierComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(QuerierName(opts.Name))
	serviceName := serviceNameQuerierHTTP(opts.Name)
	lokiEndpoint := serviceMonitorEndpoint(lokiHTTPPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewCompactorServiceMonitor creates a k8s service monitor for the compactor component
func NewCompactorServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelCompactorComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(CompactorName(opts.Name))
	serviceName := serviceNameCompactorHTTP(opts.Name)
	lokiEndpoint := serviceMonitorEndpoint(lokiHTTPPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewQueryFrontendServiceMonitor creates a k8s service monitor for the query-frontend component
func NewQueryFrontendServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelQueryFrontendComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(QueryFrontendName(opts.Name))
	serviceName := serviceNameQueryFrontendHTTP(opts.Name)
	lokiEndpoint := serviceMonitorEndpoint(lokiHTTPPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewIndexGatewayServiceMonitor creates a k8s service monitor for the index-gateway component
func NewIndexGatewayServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelIndexGatewayComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(IndexGatewayName(opts.Name))
	serviceName := serviceNameIndexGatewayHTTP(opts.Name)
	lokiEndpoint := serviceMonitorEndpoint(lokiHTTPPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewRulerServiceMonitor creates a k8s service monitor for the ruler component
func NewRulerServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelRulerComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(RulerName(opts.Name))
	serviceName := serviceNameRulerHTTP(opts.Name)
	lokiEndpoint := serviceMonitorEndpoint(lokiHTTPPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewGatewayServiceMonitor creates a k8s service monitor for the lokistack-gateway component
func NewGatewayServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelGatewayComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(GatewayName(opts.Name))
	serviceName := serviceNameGatewayHTTP(opts.Name)
	gwEndpoint := serviceMonitorEndpoint(gatewayInternalPortName, serviceName, opts.Namespace, opts.Flags.EnableTLSServiceMonitorConfig)

	sm := newServiceMonitor(opts.Namespace, serviceMonitorName, l, gwEndpoint)

	if opts.Stack.Tenants != nil {
		if err := configureServiceMonitorForMode(sm, opts.Stack.Tenants.Mode, opts.Flags); err != nil {
			return sm
		}
	}

	return sm
}

func newServiceMonitor(namespace, serviceMonitorName string, labels labels.Set, endpoint monitoringv1.Endpoint) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.ServiceMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMonitorName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			JobLabel:  labelJobComponent,
			Endpoints: []monitoringv1.Endpoint{endpoint},
			Selector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{namespace},
			},
		},
	}
}

func configureServiceMonitorPKI(podSpec *corev1.PodSpec, serviceName string) error {
	secretName := signingServiceSecretName(serviceName)
	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: secretName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretName,
					},
				},
			},
		},
	}
	secretContainerSpec := corev1.Container{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      secretName,
				ReadOnly:  false,
				MountPath: secretDirectory,
			},
		},
		Args: []string{
			"-server.http-tls-cert-path=/etc/proxy/secrets/tls.crt",
			"-server.http-tls-key-path=/etc/proxy/secrets/tls.key",
		},
	}
	uriSchemeContainerSpec := corev1.Container{
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	if err := mergo.Merge(podSpec, secretVolumeSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge volumes")
	}

	if err := mergo.Merge(&podSpec.Containers[0], secretContainerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	if err := mergo.Merge(&podSpec.Containers[0], uriSchemeContainerSpec, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	return nil
}
