package networkpolicy

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

var (
	errMissingEndpointSlices = errors.New("no endpoint slices found for target object storage service")
	errMissingWantedPort     = errors.New("couldn't resolve object storage service port to target Pod port")
)

func ServicePortToPodPort(ctx context.Context, log logr.Logger, k k8s.Client, objStore storage.Options) ([]int32, error) {
	if objStore.S3 == nil {
		return []int32{}, nil
	}
	endpoint := objStore.S3.Endpoint

	// Check if endpoint is a Kubernetes Service DNS name
	if !strings.Contains(endpoint, ".svc") {
		return []int32{}, nil
	}

	serviceName, namespace, endpointPort, https := parseServiceEndpoint(endpoint)
	if serviceName == "" || namespace == "" {
		return []int32{}, nil // We do not error as the endpoint might not point to a Kubernetes Service
	}

	service := &corev1.Service{}
	if err := k.Get(ctx, client.ObjectKey{Name: serviceName, Namespace: namespace}, service); err != nil {
		log.Info("failed to get Service for object storage", "service", serviceName, "namespace", namespace)
		return []int32{}, nil
	}

	// List EndpointSlices for the service using the standard label
	endpointSlices := &discoveryv1.EndpointSliceList{}
	if err := k.List(ctx, endpointSlices, client.InNamespace(namespace), client.MatchingLabels{discoveryv1.LabelServiceName: serviceName}); err != nil {
		log.Error(err, "failed to list endpoint slices for target object storage service", "service", serviceName, "namespace", namespace)
		return []int32{}, err
	}

	if len(endpointSlices.Items) == 0 {
		log.Error(errMissingEndpointSlices, "found no endpoint slices for service", "service", serviceName, "namespace", namespace)
		return []int32{}, errMissingEndpointSlices
	}

	var targetPort int32
	wantedPort := int32(80)
	if https {
		wantedPort = 443
	}

	if endpointPort > 0 {
		wantedPort = endpointPort // Override the wantedPort if specified in the endpoint
		for _, slice := range endpointSlices.Items {
			for _, p := range slice.Ports {
				if p.Port != nil && *p.Port == endpointPort {
					return []int32{*p.Port}, nil // If svc and pod have the same port then return it directly
				}
			}
		}
	}

	targetPort = resolveTargetPort(service, endpointSlices, wantedPort)
	if targetPort == 0 {
		return []int32{}, errMissingWantedPort
	}

	return []int32{targetPort}, nil
}

func parseServiceEndpoint(endpoint string) (string, string, int32, bool) {
	https := strings.HasPrefix(endpoint, "https://")

	var host string
	var portStr string
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		parsedURL, err := url.Parse(endpoint)
		if err != nil {
			return "", "", 0, false
		}
		host = parsedURL.Hostname()
		portStr = parsedURL.Port()
	} else {
		// Bare hostname:port format
		host = endpoint
		sHost, sPort, err := net.SplitHostPort(endpoint)
		if err == nil {
			host, portStr = sHost, sPort
		}
	}

	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return "", "", 0, false
	}

	serviceName := parts[0]
	namespace := parts[1]

	var port int32
	if portStr != "" {
		p, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return "", "", 0, false
		}
		port = int32(p)
	}

	return serviceName, namespace, port, https
}

func resolveTargetPort(service *corev1.Service, endpointSlices *discoveryv1.EndpointSliceList, endpointPort int32) int32 {
	for _, svcPort := range service.Spec.Ports {
		if svcPort.Port == endpointPort {
			for _, slice := range endpointSlices.Items {
				for _, p := range slice.Ports {
					switch svcPort.TargetPort.Type {
					case intstr.Int:
						if p.Port != nil && *p.Port == svcPort.TargetPort.IntVal {
							return *p.Port
						}
					case intstr.String:
						if p.Name != nil && *p.Name == svcPort.TargetPort.StrVal {
							return *p.Port
						}
					}
				}
			}
			return 0
		}
	}
	return 0
}
