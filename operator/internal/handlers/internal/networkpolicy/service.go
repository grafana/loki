package networkpolicy

import (
	"context"
	"errors"
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
	errInvalidServiceEndpoint = errors.New("couldn't parse target object storage service endpoint")
	errMissingEndpointSlices  = errors.New("no endpoint slices found for target object storage service")
	errMissingTargetPort      = errors.New("couldn't resolve target object storage service port to target Pod port")
	errMissingDefaultPort     = errors.New("couldn't resolve default ports to target ports")
)

func ServicePortToPodPort(ctx context.Context, log logr.Logger, k k8s.Client, objStore storage.Options) ([]int32, error) {
	if objStore.S3 == nil {
		return []int32{}, nil
	}
	endpoint := objStore.S3.Endpoint

	// Check if endpoint contains a Kubernetes Service DNS pattern
	if !strings.Contains(endpoint, ".svc") {
		return []int32{}, nil
	}

	serviceName, namespace, endpointPort, https := parseServiceEndpoint(endpoint)
	if serviceName == "" || namespace == "" {
		return []int32{}, errInvalidServiceEndpoint
	}

	// List EndpointSlices for the service using the standard label
	endpointSlices := &discoveryv1.EndpointSliceList{}
	if err := k.List(ctx, endpointSlices, client.InNamespace(namespace), client.MatchingLabels{discoveryv1.LabelServiceName: serviceName}); err != nil {
		log.Error(err, "failed to list endpoint slices for target object storage service", "service", serviceName, "namespace", namespace)
		return []int32{}, err
	}

	if len(endpointSlices.Items) == 0 {
		log.Error(errMissingEndpointSlices, "service", serviceName, "namespace", namespace)
		return []int32{}, errMissingEndpointSlices
	}

	// Case 1: Port specified in URL
	if endpointPort > 0 {
		// If SVC and Pod have the same port then we can return it directly
		for _, slice := range endpointSlices.Items {
			for _, p := range slice.Ports {
				if p.Port != nil && *p.Port == endpointPort {
					return []int32{*p.Port}, nil
				}
			}
		}

		// Port not in EndpointSlices - it's likely a service port, need to resolve via Service
		service := &corev1.Service{}
		if err := k.Get(ctx, client.ObjectKey{Name: serviceName, Namespace: namespace}, service); err != nil {
			log.Error(err, "failed to get target object storage service", "service", serviceName, "namespace", namespace)
			return []int32{}, err
		}

		var targetPort int32
		for _, sp := range service.Spec.Ports {
			if sp.Port == endpointPort {
				targetPort = resolveServicePortToTarget(endpointSlices.Items, sp)
				if targetPort != 0 {
					break
				}
			}
		}

		if targetPort == 0 {
			return []int32{}, errMissingTargetPort
		}

		return []int32{targetPort}, nil
	}

	// Case 2: No port specified - default to 443/80 and resolve their target ports
	service := &corev1.Service{}
	if err := k.Get(ctx, client.ObjectKey{Name: serviceName, Namespace: namespace}, service); err != nil {
		log.Error(err, "failed to get service for default port resolution", "service", serviceName, "namespace", namespace)
		return []int32{}, err
	}

	defaultPort := int32(80)
	if https {
		defaultPort = int32(443)
	}

	for _, sp := range service.Spec.Ports {
		if sp.Port == defaultPort {
			targetPort := resolveServicePortToTarget(endpointSlices.Items, sp)
			if targetPort == 0 {
				return []int32{}, errMissingDefaultPort
			}
			return []int32{targetPort}, nil
		}
	}

	return []int32{}, errMissingDefaultPort
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
		if idx := strings.LastIndex(endpoint, ":"); idx != -1 {
			possiblePort := endpoint[idx+1:]
			if _, err := strconv.Atoi(possiblePort); err == nil {
				host = endpoint[:idx]
				portStr = possiblePort
			}
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
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return "", "", 0, false
		}
		port = int32(p)
	}

	return serviceName, namespace, port, https
}

func resolveServicePortToTarget(slices []discoveryv1.EndpointSlice, servicePort corev1.ServicePort) int32 {
	for _, slice := range slices {
		for _, p := range slice.Ports {
			switch servicePort.TargetPort.Type {
			case intstr.Int:
				if p.Port != nil && *p.Port == servicePort.TargetPort.IntVal {
					return *p.Port
				}
			case intstr.String:
				if p.Name != nil && *p.Name == servicePort.TargetPort.StrVal {
					return *p.Port
				}
			}
		}
	}
	return 0
}
