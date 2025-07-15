package manifests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

// Test that the service ports have matching deployment/statefulset/daemonset ports on the podspec.
func TestServicesMatchPorts(t *testing.T) {
	type test struct {
		Services   []*corev1.Service
		Containers []corev1.Container
	}
	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
	}
	sha1C := "deadbef"

	table := []test{
		{
			Containers: NewDistributorDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewDistributorGRPCService(opt),
				NewDistributorHTTPService(opt),
			},
		},
		{
			Containers: NewIngesterStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewIngesterGRPCService(opt),
				NewIngesterHTTPService(opt),
			},
		},
		{
			Containers: NewQuerierDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewQuerierGRPCService(opt),
				NewQuerierHTTPService(opt),
			},
		},
		{
			Containers: NewQueryFrontendDeployment(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewQueryFrontendGRPCService(opt),
				NewQueryFrontendHTTPService(opt),
			},
		},
		{
			Containers: NewCompactorStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewCompactorGRPCService(opt),
				NewCompactorHTTPService(opt),
			},
		},
		{
			Containers: NewGatewayDeployment(opt, sha1C).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewGatewayHTTPService(opt),
			},
		},
		{
			Containers: NewIndexGatewayStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewIndexGatewayGRPCService(opt),
				NewIndexGatewayHTTPService(opt),
			},
		},
		{
			Containers: NewRulerStatefulSet(opt).Spec.Template.Spec.Containers,
			Services: []*corev1.Service{
				NewRulerGRPCService(opt),
				NewRulerHTTPService(opt),
			},
		},
	}

	containerHasPort := func(containers []corev1.Container, port int32) bool {
		for _, container := range containers {
			for _, p := range container.Ports {
				if p.ContainerPort == port {
					return true
				}
			}
		}
		return false
	}

	for _, tst := range table {
		for _, service := range tst.Services {
			for _, port := range service.Spec.Ports {
				testName := fmt.Sprintf("%s_%d", service.GetName(), port.Port)
				t.Run(testName, func(t *testing.T) {
					t.Parallel()
					found := containerHasPort(tst.Containers, port.Port)
					assert.True(t, found, "Service port (%d) does not match any port in the defined containers", port.Port)
				})
			}
		}
	}
}

// Test that all services match the labels of their deployments/statefulsets so that we know all services will
// work when deployed.
func TestServicesMatchLabels(t *testing.T) {
	type test struct {
		Services []*corev1.Service
		Object   client.Object
	}

	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
	}
	sha1C := "deadbef"

	table := []test{
		{
			Object: NewDistributorDeployment(opt),
			Services: []*corev1.Service{
				NewDistributorGRPCService(opt),
				NewDistributorHTTPService(opt),
			},
		},
		{
			Object: NewIngesterStatefulSet(opt),
			Services: []*corev1.Service{
				NewIngesterGRPCService(opt),
				NewIngesterHTTPService(opt),
			},
		},
		{
			Object: NewQuerierDeployment(opt),
			Services: []*corev1.Service{
				NewQuerierGRPCService(opt),
				NewQuerierHTTPService(opt),
			},
		},
		{
			Object: NewQueryFrontendDeployment(opt),
			Services: []*corev1.Service{
				NewQueryFrontendGRPCService(opt),
				NewQueryFrontendHTTPService(opt),
			},
		},
		{
			Object: NewCompactorStatefulSet(opt),
			Services: []*corev1.Service{
				NewCompactorGRPCService(opt),
				NewCompactorHTTPService(opt),
			},
		},
		{
			Object: NewGatewayDeployment(opt, sha1C),
			Services: []*corev1.Service{
				NewGatewayHTTPService(opt),
			},
		},
		{
			Object: NewIndexGatewayStatefulSet(opt),
			Services: []*corev1.Service{
				NewIndexGatewayGRPCService(opt),
				NewIndexGatewayHTTPService(opt),
			},
		},
		{
			Object: NewRulerStatefulSet(opt),
			Services: []*corev1.Service{
				NewRulerGRPCService(opt),
				NewRulerHTTPService(opt),
			},
		},
	}

	for _, tst := range table {
		for _, service := range tst.Services {
			testName := fmt.Sprintf("%s_%s", tst.Object.GetName(), service.GetName())
			t.Run(testName, func(t *testing.T) {
				t.Parallel()
				for k, v := range service.Spec.Selector {
					if assert.Contains(t, tst.Object.GetLabels(), k) {
						// only assert Equal if the previous assertion is successful or this will panic
						assert.Equal(t, v, tst.Object.GetLabels()[k])
					}
				}
			})
		}
	}
}

func TestServices_WithEncryption(t *testing.T) {
	const (
		stackName = "test"
		stackNs   = "ns"
	)

	opts := Options{
		Name:      stackName,
		Namespace: stackNs,
		Gates: configv1.FeatureGates{
			HTTPEncryption: true,
			GRPCEncryption: true,
		},
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
		Timeouts: defaultTimeoutConfig,
		TLSProfile: TLSProfileSpec{
			MinTLSVersion: "VersionTLS12",
			Ciphers:       []string{"cipher1", "cipher2"},
		},
	}

	tt := []struct {
		desc             string
		buildFunc        func(Options) ([]client.Object, error)
		wantPorts        []corev1.ContainerPort
		wantVolumeMounts []corev1.VolumeMount
		wantVolumes      []corev1.Volume
	}{
		{
			desc:      "compactor",
			buildFunc: BuildCompactor,
			wantPorts: []corev1.ContainerPort{
				{
					Name:          lokiInternalHTTPPortName,
					ContainerPort: internalHTTPPort,
					Protocol:      protocolTCP,
				},
			},
			wantVolumeMounts: []corev1.VolumeMount{
				{
					Name:      serviceNameCompactorHTTP(stackName),
					ReadOnly:  false,
					MountPath: lokiServerHTTPTLSDir(),
				},
				{
					Name:      serviceNameCompactorGRPC(stackName),
					ReadOnly:  false,
					MountPath: lokiServerGRPCTLSDir(),
				},
				{
					Name:      signingCABundleName(stackName),
					ReadOnly:  false,
					MountPath: caBundleDir,
				},
			},
			wantVolumes: []corev1.Volume{
				{
					Name: serviceNameCompactorHTTP(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameCompactorHTTP(stackName),
						},
					},
				},
				{
					Name: serviceNameCompactorGRPC(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameCompactorGRPC(stackName),
						},
					},
				},
				{
					Name: signingCABundleName(stackName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: signingCABundleName(stackName),
							},
						},
					},
				},
			},
		},
		{
			desc:      "distributor",
			buildFunc: BuildDistributor,
			wantPorts: []corev1.ContainerPort{
				{
					Name:          lokiInternalHTTPPortName,
					ContainerPort: internalHTTPPort,
					Protocol:      protocolTCP,
				},
			},
			wantVolumeMounts: []corev1.VolumeMount{
				{
					Name:      serviceNameDistributorHTTP(stackName),
					ReadOnly:  false,
					MountPath: lokiServerHTTPTLSDir(),
				},
				{
					Name:      serviceNameDistributorGRPC(stackName),
					ReadOnly:  false,
					MountPath: lokiServerGRPCTLSDir(),
				},
				{
					Name:      signingCABundleName(stackName),
					ReadOnly:  false,
					MountPath: caBundleDir,
				},
			},
			wantVolumes: []corev1.Volume{
				{
					Name: serviceNameDistributorHTTP(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameDistributorHTTP(stackName),
						},
					},
				},
				{
					Name: serviceNameDistributorGRPC(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameDistributorGRPC(stackName),
						},
					},
				},
				{
					Name: signingCABundleName(stackName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: signingCABundleName(stackName),
							},
						},
					},
				},
			},
		},
		{
			desc:      "index-gateway",
			buildFunc: BuildIndexGateway,
			wantPorts: []corev1.ContainerPort{
				{
					Name:          lokiInternalHTTPPortName,
					ContainerPort: internalHTTPPort,
					Protocol:      protocolTCP,
				},
			},
			wantVolumeMounts: []corev1.VolumeMount{
				{
					Name:      serviceNameIndexGatewayHTTP(stackName),
					ReadOnly:  false,
					MountPath: lokiServerHTTPTLSDir(),
				},
				{
					Name:      serviceNameIndexGatewayGRPC(stackName),
					ReadOnly:  false,
					MountPath: lokiServerGRPCTLSDir(),
				},
				{
					Name:      signingCABundleName(stackName),
					ReadOnly:  false,
					MountPath: caBundleDir,
				},
			},
			wantVolumes: []corev1.Volume{
				{
					Name: serviceNameIndexGatewayHTTP(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameIndexGatewayHTTP(stackName),
						},
					},
				},
				{
					Name: serviceNameIndexGatewayGRPC(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameIndexGatewayGRPC(stackName),
						},
					},
				},
				{
					Name: signingCABundleName(stackName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: signingCABundleName(stackName),
							},
						},
					},
				},
			},
		},
		{
			desc:      "ingester",
			buildFunc: BuildIngester,
			wantPorts: []corev1.ContainerPort{
				{
					Name:          lokiInternalHTTPPortName,
					ContainerPort: internalHTTPPort,
					Protocol:      protocolTCP,
				},
			},
			wantVolumeMounts: []corev1.VolumeMount{
				{
					Name:      serviceNameIngesterHTTP(stackName),
					ReadOnly:  false,
					MountPath: lokiServerHTTPTLSDir(),
				},
				{
					Name:      serviceNameIngesterGRPC(stackName),
					ReadOnly:  false,
					MountPath: lokiServerGRPCTLSDir(),
				},
				{
					Name:      signingCABundleName(stackName),
					ReadOnly:  false,
					MountPath: caBundleDir,
				},
			},
			wantVolumes: []corev1.Volume{
				{
					Name: serviceNameIngesterHTTP(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameIngesterHTTP(stackName),
						},
					},
				},
				{
					Name: serviceNameIngesterGRPC(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameIngesterGRPC(stackName),
						},
					},
				},
				{
					Name: signingCABundleName(stackName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: signingCABundleName(stackName),
							},
						},
					},
				},
			},
		},
		{
			desc:      "querier",
			buildFunc: BuildQuerier,
			wantPorts: []corev1.ContainerPort{
				{
					Name:          lokiInternalHTTPPortName,
					ContainerPort: internalHTTPPort,
					Protocol:      protocolTCP,
				},
			},
			wantVolumeMounts: []corev1.VolumeMount{
				{
					Name:      serviceNameQuerierHTTP(stackName),
					ReadOnly:  false,
					MountPath: lokiServerHTTPTLSDir(),
				},
				{
					Name:      serviceNameQuerierGRPC(stackName),
					ReadOnly:  false,
					MountPath: lokiServerGRPCTLSDir(),
				},
				{
					Name:      signingCABundleName(stackName),
					ReadOnly:  false,
					MountPath: caBundleDir,
				},
			},
			wantVolumes: []corev1.Volume{
				{
					Name: serviceNameQuerierHTTP(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameQuerierHTTP(stackName),
						},
					},
				},
				{
					Name: serviceNameQuerierGRPC(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameQuerierGRPC(stackName),
						},
					},
				},
				{
					Name: signingCABundleName(stackName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: signingCABundleName(stackName),
							},
						},
					},
				},
			},
		},
		{
			desc:      "query-frontend",
			buildFunc: BuildQueryFrontend,
			wantPorts: []corev1.ContainerPort{
				{
					Name:          lokiInternalHTTPPortName,
					ContainerPort: internalHTTPPort,
					Protocol:      protocolTCP,
				},
			},
			wantVolumeMounts: []corev1.VolumeMount{
				{
					Name:      serviceNameQueryFrontendHTTP(stackName),
					ReadOnly:  false,
					MountPath: lokiServerHTTPTLSDir(),
				},
				{
					Name:      serviceNameQueryFrontendGRPC(stackName),
					ReadOnly:  false,
					MountPath: lokiServerGRPCTLSDir(),
				},
				{
					Name:      signingCABundleName(stackName),
					ReadOnly:  false,
					MountPath: caBundleDir,
				},
			},
			wantVolumes: []corev1.Volume{
				{
					Name: serviceNameQueryFrontendHTTP(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameQueryFrontendHTTP(stackName),
						},
					},
				},
				{
					Name: serviceNameQueryFrontendGRPC(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameQueryFrontendGRPC(stackName),
						},
					},
				},
				{
					Name: signingCABundleName(stackName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: signingCABundleName(stackName),
							},
						},
					},
				},
			},
		},
		{
			desc:      "ruler",
			buildFunc: BuildRuler,
			wantPorts: []corev1.ContainerPort{
				{
					Name:          lokiInternalHTTPPortName,
					ContainerPort: internalHTTPPort,
					Protocol:      protocolTCP,
				},
			},
			wantVolumeMounts: []corev1.VolumeMount{
				{
					Name:      serviceNameRulerHTTP(stackName),
					ReadOnly:  false,
					MountPath: lokiServerHTTPTLSDir(),
				},
				{
					Name:      serviceNameRulerGRPC(stackName),
					ReadOnly:  false,
					MountPath: lokiServerGRPCTLSDir(),
				},
				{
					Name:      signingCABundleName(stackName),
					ReadOnly:  false,
					MountPath: caBundleDir,
				},
			},
			wantVolumes: []corev1.Volume{
				{
					Name: serviceNameRulerHTTP(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameRulerHTTP(stackName),
						},
					},
				},
				{
					Name: serviceNameRulerGRPC(stackName),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: serviceNameRulerGRPC(stackName),
						},
					},
				},
				{
					Name: signingCABundleName(stackName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: signingCABundleName(stackName),
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tt {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			objs, err := test.buildFunc(opts)
			require.NoError(t, err)

			var pod *corev1.PodSpec
			switch o := objs[0].(type) {
			case *appsv1.Deployment:
				pod = &o.Spec.Template.Spec
			case *appsv1.StatefulSet:
				pod = &o.Spec.Template.Spec
			default:
				t.Fatal("Wrong object type given")
			}

			isEncryptionRelated := func(s string) bool {
				return strings.Contains(s, "internal-server") || // Healthcheck server
					strings.Contains(s, "client") || // Client certificates
					strings.Contains(s, "-http") || // Serving HTTP certificates
					strings.Contains(s, "-grpc") || // Serving GRPC certificates
					strings.Contains(s, "ca") // Certificate authorities
			}

			// Check ports not missing
			for _, port := range test.wantPorts {
				require.Contains(t, pod.Containers[0].Ports, port)
			}

			// Check mounts not missing
			for _, mount := range test.wantVolumeMounts {
				require.Contains(t, pod.Containers[0].VolumeMounts, mount)
			}
			for _, mount := range pod.Containers[0].VolumeMounts {
				if isEncryptionRelated(mount.Name) {
					require.Contains(t, test.wantVolumeMounts, mount)
				}
			}

			// Check volumes not missing
			for _, volume := range test.wantVolumes {
				require.Contains(t, pod.Volumes, volume)
			}
			for _, volume := range pod.Volumes {
				if isEncryptionRelated(volume.Name) {
					require.Contains(t, test.wantVolumes, volume)
				}
			}
		})
	}
}
