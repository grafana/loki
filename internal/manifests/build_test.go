package manifests

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests/internal"
	"github.com/stretchr/testify/require"
)

func TestApplyUserOptions_OverrideDefaults(t *testing.T) {
	allSizes := []lokiv1beta1.LokiStackSizeType{
		lokiv1beta1.SizeOneXExtraSmall,
		lokiv1beta1.SizeOneXSmall,
		lokiv1beta1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		opt := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1beta1.LokiStackSpec{
				Size: size,
				Template: &lokiv1beta1.LokiTemplateSpec{
					Distributor: &lokiv1beta1.LokiComponentSpec{
						Replicas: 42,
					},
				},
			},
		}
		err := ApplyDefaultSettings(&opt)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)
		require.Equal(t, defs.Size, opt.Stack.Size)
		require.Equal(t, defs.Limits, opt.Stack.Limits)
		require.Equal(t, defs.ReplicationFactor, opt.Stack.ReplicationFactor)
		require.Equal(t, defs.ManagementState, opt.Stack.ManagementState)
		require.Equal(t, defs.Template.Ingester, opt.Stack.Template.Ingester)
		require.Equal(t, defs.Template.Querier, opt.Stack.Template.Querier)
		require.Equal(t, defs.Template.QueryFrontend, opt.Stack.Template.QueryFrontend)

		// Require distributor replicas to be set by user overwrite
		require.NotEqual(t, defs.Template.Distributor.Replicas, opt.Stack.Template.Distributor.Replicas)

		// Require distributor tolerations and nodeselectors to use defaults
		require.Equal(t, defs.Template.Distributor.Tolerations, opt.Stack.Template.Distributor.Tolerations)
		require.Equal(t, defs.Template.Distributor.NodeSelector, opt.Stack.Template.Distributor.NodeSelector)
	}
}

func TestApplyUserOptions_AlwaysSetCompactorReplicasToOne(t *testing.T) {
	allSizes := []lokiv1beta1.LokiStackSizeType{
		lokiv1beta1.SizeOneXExtraSmall,
		lokiv1beta1.SizeOneXSmall,
		lokiv1beta1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		opt := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1beta1.LokiStackSpec{
				Size: size,
				Template: &lokiv1beta1.LokiTemplateSpec{
					Compactor: &lokiv1beta1.LokiComponentSpec{
						Replicas: 2,
					},
				},
			},
		}
		err := ApplyDefaultSettings(&opt)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)

		// Require compactor to be reverted to 1 replica
		require.Equal(t, defs.Template.Compactor, opt.Stack.Template.Compactor)
	}
}

func TestBuildAll_WithFeatureFlags_EnableServiceMonitors(t *testing.T) {
	type test struct {
		desc         string
		MonitorCount int
		BuildOptions Options
	}

	table := []test{
		{
			desc:         "no service monitors created",
			MonitorCount: 0,
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: false,
					EnableServiceMonitors:           false,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
		{
			desc:         "service monitor per component created",
			MonitorCount: 5,
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: false,
					EnableServiceMonitors:           true,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
	}

	for _, tst := range table {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)

			objects, buildErr := BuildAll(tst.BuildOptions)

			require.NoError(t, buildErr)
			require.Equal(t, tst.MonitorCount, serviceMonitorCount(objects))
		})
	}
}

func TestBuildAll_WithFeatureFlags_EnableCertificateSigningService(t *testing.T) {
	type test struct {
		desc         string
		BuildOptions Options
	}

	table := []test{
		{
			desc: "disabled certificate signing service",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: false,
					EnableServiceMonitors:           false,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
		{
			desc: "enabled certificate signing service for every http service",
			BuildOptions: Options{
				Name:      "test",
				Namespace: "test",
				Stack: lokiv1beta1.LokiStackSpec{
					Size: lokiv1beta1.SizeOneXSmall,
				},
				Flags: FeatureFlags{
					EnableCertificateSigningService: true,
					EnableServiceMonitors:           false,
					EnableTLSServiceMonitorConfig:   false,
				},
			},
		},
	}

	for _, tst := range table {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			err := ApplyDefaultSettings(&tst.BuildOptions)
			require.NoError(t, err)

			httpServices := []*corev1.Service{
				NewDistributorHTTPService(tst.BuildOptions),
				NewIngesterHTTPService(tst.BuildOptions),
				NewQuerierHTTPService(tst.BuildOptions),
				NewQueryFrontendHTTPService(tst.BuildOptions),
				NewCompactorHTTPService(tst.BuildOptions),
			}

			for _, service := range httpServices {
				if !tst.BuildOptions.Flags.EnableCertificateSigningService {
					require.Equal(t, service.ObjectMeta.Annotations, map[string]string{})
				} else {
					require.NotNil(t, service.ObjectMeta.Annotations["service.beta.openshift.io/serving-cert-secret-name"])
				}
			}
		})
	}
}

func serviceMonitorCount(objects []client.Object) int {
	monitors := 0
	for _, obj := range objects {
		if obj.GetObjectKind().GroupVersionKind().Kind == "ServiceMonitor" {
			monitors++
		}
	}
	return monitors
}
