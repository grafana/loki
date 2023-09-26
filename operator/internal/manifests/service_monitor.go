package manifests

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	lokiEndpoint := lokiServiceMonitorEndpoint(opts.Name, lokiHTTPPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewIngesterServiceMonitor creates a k8s service monitor for the ingester component
func NewIngesterServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelIngesterComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(IngesterName(opts.Name))
	serviceName := serviceNameIngesterHTTP(opts.Name)
	lokiEndpoint := lokiServiceMonitorEndpoint(opts.Name, lokiHTTPPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewQuerierServiceMonitor creates a k8s service monitor for the querier component
func NewQuerierServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelQuerierComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(QuerierName(opts.Name))
	serviceName := serviceNameQuerierHTTP(opts.Name)
	lokiEndpoint := lokiServiceMonitorEndpoint(opts.Name, lokiHTTPPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewCompactorServiceMonitor creates a k8s service monitor for the compactor component
func NewCompactorServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelCompactorComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(CompactorName(opts.Name))
	serviceName := serviceNameCompactorHTTP(opts.Name)
	lokiEndpoint := lokiServiceMonitorEndpoint(opts.Name, lokiHTTPPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewQueryFrontendServiceMonitor creates a k8s service monitor for the query-frontend component
func NewQueryFrontendServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelQueryFrontendComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(QueryFrontendName(opts.Name))
	serviceName := serviceNameQueryFrontendHTTP(opts.Name)
	lokiEndpoint := lokiServiceMonitorEndpoint(opts.Name, lokiHTTPPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewIndexGatewayServiceMonitor creates a k8s service monitor for the index-gateway component
func NewIndexGatewayServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelIndexGatewayComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(IndexGatewayName(opts.Name))
	serviceName := serviceNameIndexGatewayHTTP(opts.Name)
	lokiEndpoint := lokiServiceMonitorEndpoint(opts.Name, lokiHTTPPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewRulerServiceMonitor creates a k8s service monitor for the ruler component
func NewRulerServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelRulerComponent, opts.Name)

	serviceMonitorName := serviceMonitorName(RulerName(opts.Name))
	serviceName := serviceNameRulerHTTP(opts.Name)
	lokiEndpoint := lokiServiceMonitorEndpoint(opts.Name, lokiHTTPPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	return newServiceMonitor(opts.Namespace, serviceMonitorName, l, lokiEndpoint)
}

// NewGatewayServiceMonitor creates a k8s service monitor for the lokistack-gateway component
func NewGatewayServiceMonitor(opts Options) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelGatewayComponent, opts.Name)

	gatewayName := GatewayName(opts.Name)
	serviceMonitorName := serviceMonitorName(gatewayName)
	serviceName := serviceNameGatewayHTTP(opts.Name)
	gwEndpoint := gatewayServiceMonitorEndpoint(gatewayName, gatewayInternalPortName, serviceName, opts.Namespace, opts.Gates.ServiceMonitorTLSEndpoints)

	sm := newServiceMonitor(opts.Namespace, serviceMonitorName, l, gwEndpoint)

	if opts.Stack.Tenants != nil {
		if err := configureGatewayServiceMonitorForMode(sm, opts); err != nil {
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
