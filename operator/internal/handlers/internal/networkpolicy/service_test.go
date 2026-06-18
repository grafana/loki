package networkpolicy

import (
	"context"
	"testing"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
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

			gotPorts, err := portToPodPort(context.Background(), logr.Discard(), k, storage.Options{
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

func TestResolveTargetPort(t *testing.T) {
	for _, tt := range []struct {
		name         string
		service      *corev1.Service
		slices       *discoveryv1.EndpointSliceList
		endpointPort int32
		expectedPort int32
	}{
		{
			name: "simple matching port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 80,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 80,
							},
						},
					},
				},
			},
			slices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						Ports: []discoveryv1.EndpointPort{
							{Port: ptr.To(int32(80))},
						},
					},
				},
			},
			endpointPort: 80,
			expectedPort: 80,
		},
		{
			name: "targetPort diff from svc port but matching port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 80,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 8080,
							},
						},
					},
				},
			},
			slices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						Ports: []discoveryv1.EndpointPort{
							{Port: ptr.To(int32(8080))},
						},
					},
				},
			},
			endpointPort: 80,
			expectedPort: 8080,
		},
		{
			name: "targetPort diff from svc port but matching name",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 80,
							TargetPort: intstr.IntOrString{
								Type:   intstr.String,
								StrVal: "http",
							},
						},
					},
				},
			},
			slices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						Ports: []discoveryv1.EndpointPort{
							{
								Port: ptr.To(int32(8080)),
								Name: ptr.To("http"),
							},
						},
					},
				},
			},
			endpointPort: 80,
			expectedPort: 8080,
		},
		{
			name: "no matching port or name",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 80,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 443,
							},
						},
					},
				},
			},
			slices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						Ports: []discoveryv1.EndpointPort{
							{
								Port: ptr.To(int32(8080)),
								Name: ptr.To("http"),
							},
						},
					},
				},
			},
			endpointPort: 80,
			expectedPort: 0,
		},
		{
			name: "no matching service port",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 443,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 6443,
							},
						},
					},
				},
			},
			slices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						Ports: []discoveryv1.EndpointPort{
							{Port: ptr.To(int32(6443))},
						},
					},
				},
			},
			endpointPort: 80,
			expectedPort: 0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTargetPort(tt.service, tt.slices, tt.endpointPort)
			require.Equal(t, tt.expectedPort, got)
		})
	}
}

func TestDetermineObjectStoragePorts(t *testing.T) {
	for _, tt := range []struct {
		name          string
		objStore      storage.Options
		stack         lokiv1.LokiStack
		service       *corev1.Service
		endpointSlice *discoveryv1.EndpointSlice
		featureGates  configv1.FeatureGates
		expectedPorts []int32
		expectedError bool
	}{
		{
			name: "kubernetes service endpoint with explicit port",
			objStore: storage.Options{
				S3: &storage.S3StorageConfig{
					Endpoint: "http://minio.openshift-logging.svc.cluster.local:9001",
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minio",
					Namespace: "openshift-logging",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 9001,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9000,
							},
						},
					},
				},
			},
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minio-slice",
					Namespace: "openshift-logging",
					Labels: map[string]string{
						discoveryv1.LabelServiceName: "minio",
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{Port: ptr.To(int32(9000))},
				},
			},
			expectedPorts: []int32{9000},
		},
		{
			name: "static S3 endpoint with custom port",
			objStore: storage.Options{
				S3: &storage.S3StorageConfig{
					Endpoint: "https://s3.example.com:9443",
				},
			},
			expectedPorts: []int32{9443},
		},
		{
			name: "static S3 endpoint without port",
			objStore: storage.Options{
				S3: &storage.S3StorageConfig{
					Endpoint: "https://s3.amazonaws.com",
				},
			},
			expectedPorts: []int32{443},
		},
		{
			name: "AlibabaCloud endpoint with custom port",
			objStore: storage.Options{
				AlibabaCloud: &storage.AlibabaCloudStorageConfig{
					Endpoint: "https://oss.aliyun.com:8080",
				},
			},
			expectedPorts: []int32{8080},
		},
		{
			name: "Swift endpoint with auth URL port",
			objStore: storage.Options{
				Swift: &storage.SwiftStorageConfig{
					AuthURL: "http://swift.example.com:5000/v3",
				},
			},
			expectedPorts: []int32{5000, 443},
		},
		{
			name: "Swift endpoint with OpenShift feature gate enabled (no logging mode)",
			objStore: storage.Options{
				Swift: &storage.SwiftStorageConfig{
					AuthURL: "http://swift.example.com:5000/v3",
				},
			},
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled: true,
				},
			},
			expectedPorts: []int32{5000, 13808},
		},
		{
			name: "Swift endpoint without auth URL port",
			objStore: storage.Options{
				Swift: &storage.SwiftStorageConfig{
					AuthURL: "http://swift.example.com/v3",
				},
			},
			expectedPorts: []int32{443},
		},
		{
			name: "S3 with HTTP and HTTPS proxy",
			objStore: storage.Options{
				S3: &storage.S3StorageConfig{
					Endpoint: "https://s3.amazonaws.com",
				},
			},
			stack: lokiv1.LokiStack{
				Spec: lokiv1.LokiStackSpec{
					Proxy: &lokiv1.ClusterProxy{
						HTTPProxy:  "http://proxy.example.com:8080",
						HTTPSProxy: "https://proxy.example.com:8443",
					},
				},
			},
			expectedPorts: []int32{443, 8080, 8443},
		},
		{
			name: "S3 with only HTTP proxy",
			objStore: storage.Options{
				S3: &storage.S3StorageConfig{
					Endpoint: "https://s3.amazonaws.com",
				},
			},
			stack: lokiv1.LokiStack{
				Spec: lokiv1.LokiStackSpec{
					Proxy: &lokiv1.ClusterProxy{
						HTTPProxy: "http://proxy.example.com:3128",
					},
				},
			},
			expectedPorts: []int32{443, 3128},
		},
		{
			name: "Kubernetes Service with proxy",
			objStore: storage.Options{
				S3: &storage.S3StorageConfig{
					Endpoint: "http://minio.default.svc.cluster.local:9000",
				},
			},
			stack: lokiv1.LokiStack{
				Spec: lokiv1.LokiStackSpec{
					Proxy: &lokiv1.ClusterProxy{
						HTTPProxy: "http://proxy:8080",
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minio",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 9000,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9000,
							},
						},
					},
				},
			},
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minio-slice",
					Namespace: "default",
					Labels: map[string]string{
						discoveryv1.LabelServiceName: "minio",
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{Port: ptr.To(int32(9000))},
				},
			},
			expectedPorts: []int32{9000, 8080},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var k *fake.ClientBuilder
			if tt.service != nil && tt.endpointSlice != nil {
				k = fake.NewClientBuilder().
					WithObjects(tt.service, tt.endpointSlice).
					WithScheme(scheme.Scheme)
			} else {
				k = fake.NewClientBuilder().WithScheme(scheme.Scheme)
			}

			logger := log.NewLogger("")

			ports, err := DetermineObjectStoragePorts(context.Background(), logger, k.Build(), tt.objStore, tt.stack, tt.featureGates)

			if tt.expectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expectedPorts, ports)
		})
	}
}
