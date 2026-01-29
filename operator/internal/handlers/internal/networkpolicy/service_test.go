package networkpolicy

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/grafana/loki/operator/internal/manifests/storage"
)

var (
	_ = corev1.AddToScheme(scheme.Scheme)
	_ = discoveryv1.AddToScheme(scheme.Scheme)
)

func TestServicePortToPodPort(t *testing.T) {
	for _, tt := range []struct {
		name          string
		endpoint      string
		expectedPort  int32
		expectedError bool
	}{
		{
			name:         "shortest svc endpoint without port",
			endpoint:     "minio.test.svc",
			expectedPort: 8080,
		},
		{
			name:         "svc endpoint without port",
			endpoint:     "minio.test.svc.cluster.local",
			expectedPort: 8080,
		},
		{
			name:         "https shortest svc endpoint",
			endpoint:     "https://minio.test.svc",
			expectedPort: 6443,
		},
		{
			name:         "https svc endpoint with port",
			endpoint:     "https://minio.test.svc.cluster.local:443",
			expectedPort: 6443,
		},
		{
			name:         "https svc endpoint with name",
			endpoint:     "https://minio.test.svc.cluster.local:444",
			expectedPort: 6443,
		},
		{
			name:          "https svc endpoint with invalid port",
			endpoint:      "https://minio.test.svc.cluster.local:9999",
			expectedPort:  9999,
			expectedError: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minio",
					Namespace: "test",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 443,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 6443,
							},
						},
						{
							Port: 444,
							TargetPort: intstr.IntOrString{
								Type:   intstr.String,
								StrVal: "https",
							},
						},
						{
							Port: 80,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 8080,
							},
						},
					},
				},
			}

			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minio-endpoint-slice",
					Namespace: "test",
					Labels: map[string]string{
						discoveryv1.LabelServiceName: "minio",
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: ptr.To(int32(8080)),
					},
					{
						Port: ptr.To(int32(6443)),
						Name: ptr.To("https"),
					},
				},
			}

			k := fake.NewClientBuilder().WithObjects(service, endpointSlice).
				WithScheme(scheme.Scheme).
				Build()

			gotPorts, err := ServicePortToPodPort(context.TODO(), logr.Discard(), k, storage.Options{
				S3: &storage.S3StorageConfig{
					Endpoint: tt.endpoint,
				},
			})
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []int32{tt.expectedPort}, gotPorts)
		})
	}
}

func TestParseServiceEndpoint(t *testing.T) {
	for _, tt := range []struct {
		name                string
		endpoint            string
		expectedServiceName string
		expectedNamespace   string
		expectedPort        int32
		expectedHTTPS       bool
	}{
		{
			name:                "shortest svc endpoint without port",
			endpoint:            "minio.test.svc",
			expectedServiceName: "minio",
			expectedNamespace:   "test",
			expectedPort:        0,
			expectedHTTPS:       false,
		},
		{
			name:                "shortest svc endpoint with port",
			endpoint:            "minio.test.svc:443",
			expectedServiceName: "minio",
			expectedNamespace:   "test",
			expectedPort:        443,
			expectedHTTPS:       false,
		},
		{
			name:                "svc endpoint with port",
			endpoint:            "minio.test.svc.cluster.local:443",
			expectedServiceName: "minio",
			expectedNamespace:   "test",
			expectedPort:        443,
			expectedHTTPS:       false,
		},
		{
			name:                "http svc endpoint without port",
			endpoint:            "http://minio.test.svc.cluster.local",
			expectedServiceName: "minio",
			expectedNamespace:   "test",
			expectedPort:        0,
			expectedHTTPS:       false,
		},
		{
			name:                "https svc endpoint without port",
			endpoint:            "https://minio.test.svc.cluster.local",
			expectedServiceName: "minio",
			expectedNamespace:   "test",
			expectedPort:        0,
			expectedHTTPS:       true,
		},
		{
			name:                "https svc endpoint with port",
			endpoint:            "https://minio.test.svc.cluster.local:443",
			expectedServiceName: "minio",
			expectedNamespace:   "test",
			expectedPort:        443,
			expectedHTTPS:       true,
		},
		{
			name:                "shortest https svc endpoint with port",
			endpoint:            "https://minio.test.svc:443",
			expectedServiceName: "minio",
			expectedNamespace:   "test",
			expectedPort:        443,
			expectedHTTPS:       true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gotServiceName, gotNamespace, gotPort, gotHTTPS := parseServiceEndpoint(tt.endpoint)
			require.Equal(t, tt.expectedServiceName, gotServiceName)
			require.Equal(t, tt.expectedNamespace, gotNamespace)
			require.Equal(t, tt.expectedPort, gotPort)
			require.Equal(t, tt.expectedHTTPS, gotHTTPS)
		})
	}
}

func TestResolveServicePortToTarget(t *testing.T) {
	for _, tt := range []struct {
		name         string
		slices       []discoveryv1.EndpointSlice
		servicePort  corev1.ServicePort
		expectedPort int32
	}{
		{
			name: "simple matching port",
			slices: []discoveryv1.EndpointSlice{
				{
					Ports: []discoveryv1.EndpointPort{
						{
							Port: ptr.To(int32(80)),
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 80,
				},
			},
			expectedPort: 80,
		},
		{
			name: "targetPort diff from svc port but matching port",
			slices: []discoveryv1.EndpointSlice{
				{
					Ports: []discoveryv1.EndpointPort{
						{
							Port: ptr.To(int32(8080)),
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 8080,
				},
			},
			expectedPort: 8080,
		},
		{
			name: "targetPort diff from svc port but matching name",
			slices: []discoveryv1.EndpointSlice{
				{
					Ports: []discoveryv1.EndpointPort{
						{
							Port: ptr.To(int32(8080)),
							Name: ptr.To("http"),
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "http",
				},
			},
			expectedPort: 8080,
		},
		{
			name: "no matching port or name",
			slices: []discoveryv1.EndpointSlice{
				{
					Ports: []discoveryv1.EndpointPort{
						{
							Port: ptr.To(int32(8080)),
							Name: ptr.To("http"),
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 443,
				},
			},
			expectedPort: 0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveServicePortToTarget(tt.slices, tt.servicePort)
			require.Equal(t, tt.expectedPort, got)
		})
	}
}
