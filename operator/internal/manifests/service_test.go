package manifests

import (
	"fmt"
	"strings"
	"testing"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
				// rescope for t.Parallel
				tst, service, port := tst, service, port
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
			// rescope for t.Parallel()
			tst, service := tst, service

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
		TLSProfile: TLSProfileSpec{
			MinTLSVersion: "VersionTLS12",
			Ciphers:       []string{"cipher1", "cipher2"},
		},
	}

	tt := []struct {
		desc             string
		buildFunc        func(Options) ([]client.Object, error)
		wantArgs         []string
		wantPorts        []corev1.ContainerPort
		wantVolumeMounts []corev1.VolumeMount
		wantVolumes      []corev1.Volume
	}{
		{
			desc:      "compactor",
			buildFunc: BuildCompactor,
			wantArgs: []string{
				"-internal-server.enable=true",
				"-internal-server.http-listen-address=",
				fmt.Sprintf("-internal-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-internal-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-internal-server.http-tls-cipher-suites=cipher1,cipher2",
				"-internal-server.http-tls-min-version=VersionTLS12",
				"-server.tls-cipher-suites=cipher1,cipher2",
				"-server.tls-min-version=VersionTLS12",
				fmt.Sprintf("-server.http-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-server.http-tls-client-auth=RequireAndVerifyClientCert",
				fmt.Sprintf("-server.grpc-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.grpc-tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-server.grpc-tls-key-path=%s", lokiServerGRPCTLSKey()),
				"-server.grpc-tls-client-auth=RequireAndVerifyClientCert",
			},
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
			wantArgs: []string{
				"-ingester.client.tls-enabled=true",
				fmt.Sprintf("-ingester.client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-ingester.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-ingester.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-ingester.client.tls-server-name=%s", fqdn(serviceNameIngesterGRPC(stackName), stackNs)),
				"-ingester.client.tls-min-version=VersionTLS12",
				"-ingester.client.tls-cipher-suites=cipher1,cipher2",
				"-internal-server.enable=true",
				"-internal-server.http-listen-address=",
				fmt.Sprintf("-internal-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-internal-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-internal-server.http-tls-cipher-suites=cipher1,cipher2",
				"-internal-server.http-tls-min-version=VersionTLS12",
				"-server.tls-cipher-suites=cipher1,cipher2",
				"-server.tls-min-version=VersionTLS12",
				fmt.Sprintf("-server.http-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-server.http-tls-client-auth=RequireAndVerifyClientCert",
				fmt.Sprintf("-server.grpc-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.grpc-tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-server.grpc-tls-key-path=%s", lokiServerGRPCTLSKey()),
				"-server.grpc-tls-client-auth=RequireAndVerifyClientCert",
			},
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
			wantArgs: []string{
				"-internal-server.enable=true",
				"-internal-server.http-listen-address=",
				fmt.Sprintf("-internal-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-internal-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-internal-server.http-tls-cipher-suites=cipher1,cipher2",
				"-internal-server.http-tls-min-version=VersionTLS12",
				"-server.tls-cipher-suites=cipher1,cipher2",
				"-server.tls-min-version=VersionTLS12",
				fmt.Sprintf("-server.http-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-server.http-tls-client-auth=RequireAndVerifyClientCert",
				fmt.Sprintf("-server.grpc-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.grpc-tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-server.grpc-tls-key-path=%s", lokiServerGRPCTLSKey()),
				"-server.grpc-tls-client-auth=RequireAndVerifyClientCert",
			},
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
			wantArgs: []string{
				"-ingester.client.tls-enabled=true",
				fmt.Sprintf("-ingester.client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-ingester.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-ingester.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-ingester.client.tls-server-name=%s", fqdn(serviceNameIngesterGRPC(stackName), stackNs)),
				"-ingester.client.tls-min-version=VersionTLS12",
				"-ingester.client.tls-cipher-suites=cipher1,cipher2",
				"-boltdb.shipper.index-gateway-client.grpc.tls-enabled=true",
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-server-name=%s", fqdn(serviceNameIndexGatewayGRPC(stackName), stackNs)),
				"-boltdb.shipper.index-gateway-client.grpc.tls-min-version=VersionTLS12",
				"-boltdb.shipper.index-gateway-client.grpc.tls-cipher-suites=cipher1,cipher2",
				"-internal-server.enable=true",
				"-internal-server.http-listen-address=",
				fmt.Sprintf("-internal-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-internal-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-internal-server.http-tls-cipher-suites=cipher1,cipher2",
				"-internal-server.http-tls-min-version=VersionTLS12",
				"-server.tls-cipher-suites=cipher1,cipher2",
				"-server.tls-min-version=VersionTLS12",
				fmt.Sprintf("-server.http-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-server.http-tls-client-auth=RequireAndVerifyClientCert",
				fmt.Sprintf("-server.grpc-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.grpc-tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-server.grpc-tls-key-path=%s", lokiServerGRPCTLSKey()),
				"-server.grpc-tls-client-auth=RequireAndVerifyClientCert",
			},
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
			wantArgs: []string{
				"-ingester.client.tls-enabled=true",
				fmt.Sprintf("-ingester.client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-ingester.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-ingester.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-ingester.client.tls-server-name=%s", fqdn(serviceNameIngesterGRPC(stackName), stackNs)),
				"-ingester.client.tls-min-version=VersionTLS12",
				"-ingester.client.tls-cipher-suites=cipher1,cipher2",
				"-querier.frontend-client.tls-enabled=true",
				fmt.Sprintf("-querier.frontend-client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-querier.frontend-client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-querier.frontend-client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-querier.frontend-client.tls-server-name=%s", fqdn(serviceNameQueryFrontendGRPC(stackName), stackNs)),
				"-querier.frontend-client.tls-min-version=VersionTLS12",
				"-querier.frontend-client.tls-cipher-suites=cipher1,cipher2",
				"-boltdb.shipper.compactor.client.tls-enabled=true",
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-server-name=%s", fqdn(serviceNameCompactorHTTP(stackName), stackNs)),
				"-boltdb.shipper.compactor.client.tls-min-version=VersionTLS12",
				"-boltdb.shipper.compactor.client.tls-cipher-suites=cipher1,cipher2",
				"-boltdb.shipper.index-gateway-client.grpc.tls-enabled=true",
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-server-name=%s", fqdn(serviceNameIndexGatewayGRPC(stackName), stackNs)),
				"-boltdb.shipper.index-gateway-client.grpc.tls-min-version=VersionTLS12",
				"-boltdb.shipper.index-gateway-client.grpc.tls-cipher-suites=cipher1,cipher2",
				"-internal-server.enable=true",
				"-internal-server.http-listen-address=",
				fmt.Sprintf("-internal-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-internal-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-internal-server.http-tls-cipher-suites=cipher1,cipher2",
				"-internal-server.http-tls-min-version=VersionTLS12",
				"-server.tls-cipher-suites=cipher1,cipher2",
				"-server.tls-min-version=VersionTLS12",
				fmt.Sprintf("-server.http-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-server.http-tls-client-auth=RequireAndVerifyClientCert",
				fmt.Sprintf("-server.grpc-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.grpc-tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-server.grpc-tls-key-path=%s", lokiServerGRPCTLSKey()),
				"-server.grpc-tls-client-auth=RequireAndVerifyClientCert",
			},
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
			wantArgs: []string{
				"-frontend.tail-tls-config.tls-min-version=VersionTLS12",
				"-frontend.tail-tls-config.tls-cipher-suites=cipher1,cipher2",
				fmt.Sprintf("-frontend.tail-tls-config.tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-frontend.tail-tls-config.tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-frontend.tail-proxy-url=https://test-querier-http.ns.svc.cluster.local:3100",
				fmt.Sprintf("-frontend.tail-tls-config.tls-ca-path=%s", signingCAPath()),
				"-internal-server.enable=true",
				"-internal-server.http-listen-address=",
				fmt.Sprintf("-internal-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-internal-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-internal-server.http-tls-cipher-suites=cipher1,cipher2",
				"-internal-server.http-tls-min-version=VersionTLS12",
				"-server.tls-cipher-suites=cipher1,cipher2",
				"-server.tls-min-version=VersionTLS12",
				fmt.Sprintf("-server.http-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-server.http-tls-client-auth=RequireAndVerifyClientCert",
				fmt.Sprintf("-server.grpc-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.grpc-tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-server.grpc-tls-key-path=%s", lokiServerGRPCTLSKey()),
				"-server.grpc-tls-client-auth=RequireAndVerifyClientCert",
			},
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
			wantArgs: []string{
				"-boltdb.shipper.compactor.client.tls-enabled=true",
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-boltdb.shipper.compactor.client.tls-server-name=%s", fqdn(serviceNameCompactorHTTP(stackName), stackNs)),
				"-boltdb.shipper.compactor.client.tls-min-version=VersionTLS12",
				"-boltdb.shipper.compactor.client.tls-cipher-suites=cipher1,cipher2",
				"-boltdb.shipper.index-gateway-client.grpc.tls-enabled=true",
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-server-name=%s", fqdn(serviceNameIndexGatewayGRPC(stackName), stackNs)),
				"-boltdb.shipper.index-gateway-client.grpc.tls-min-version=VersionTLS12",
				"-boltdb.shipper.index-gateway-client.grpc.tls-cipher-suites=cipher1,cipher2",
				"-ingester.client.tls-enabled=true",
				fmt.Sprintf("-ingester.client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-ingester.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-ingester.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-ingester.client.tls-server-name=%s", fqdn(serviceNameIngesterGRPC(stackName), stackNs)),
				"-ingester.client.tls-min-version=VersionTLS12",
				"-ingester.client.tls-cipher-suites=cipher1,cipher2",
				"-ruler.client.tls-enabled=true",
				fmt.Sprintf("-ruler.client.tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-ruler.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-ruler.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
				fmt.Sprintf("-ruler.client.tls-server-name=%s", fqdn(serviceNameRulerGRPC(stackName), stackNs)),
				"-ruler.client.tls-min-version=VersionTLS12",
				"-ruler.client.tls-cipher-suites=cipher1,cipher2",
				"-internal-server.enable=true",
				"-internal-server.http-listen-address=",
				fmt.Sprintf("-internal-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-internal-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-internal-server.http-tls-cipher-suites=cipher1,cipher2",
				"-internal-server.http-tls-min-version=VersionTLS12",
				"-server.tls-cipher-suites=cipher1,cipher2",
				"-server.tls-min-version=VersionTLS12",
				fmt.Sprintf("-server.http-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.http-tls-cert-path=%s", lokiServerHTTPTLSCert()),
				fmt.Sprintf("-server.http-tls-key-path=%s", lokiServerHTTPTLSKey()),
				"-server.http-tls-client-auth=RequireAndVerifyClientCert",
				fmt.Sprintf("-server.grpc-tls-ca-path=%s", signingCAPath()),
				fmt.Sprintf("-server.grpc-tls-cert-path=%s", lokiServerGRPCTLSCert()),
				fmt.Sprintf("-server.grpc-tls-key-path=%s", lokiServerGRPCTLSKey()),
				"-server.grpc-tls-client-auth=RequireAndVerifyClientCert",
			},
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
		test := test
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

			// Check args not missing
			for _, arg := range test.wantArgs {
				require.Contains(t, pod.Containers[0].Args, arg)
			}
			for _, arg := range pod.Containers[0].Args {
				if isEncryptionRelated(arg) {
					require.Contains(t, test.wantArgs, arg)
				}
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
