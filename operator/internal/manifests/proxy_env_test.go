package manifests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func TestContainerEnvVars_ReadVarsFromCustomResource(t *testing.T) {
	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Proxy: &lokiv1.ClusterProxy{
				HTTPProxy:  "http-test",
				HTTPSProxy: "https-test",
				NoProxy:    "noproxy-test",
			},
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

	for _, cs := range lokiContainers(t, opt) {
		for _, c := range cs {
			require.Contains(t, c.Env, corev1.EnvVar{Name: httpProxyKey, Value: "http-test"},
				"missing envVar HTTP_PROXY for: %s", c.Name)
			require.Contains(t, c.Env, corev1.EnvVar{Name: strings.ToLower(httpProxyKey), Value: "http-test"},
				"missing envVar http_proxy for: %s", c.Name)
			require.Contains(t, c.Env, corev1.EnvVar{Name: httpsProxyKey, Value: "https-test"},
				"missing envVar HTTPS_PROXY for: %s", c.Name)
			require.Contains(t, c.Env, corev1.EnvVar{Name: strings.ToLower(httpsProxyKey), Value: "https-test"},
				"missing envVar https_proxy for: %s", c.Name)
			require.Contains(t, c.Env, corev1.EnvVar{Name: noProxyKey, Value: "noproxy-test"},
				"missing envVar NO_PROXY for: %s", c.Name)
			require.Contains(t, c.Env, corev1.EnvVar{Name: strings.ToLower(noProxyKey), Value: "noproxy-test"},
				"missing envVar no_proxy for: %s", c.Name)
		}
	}
}

func lokiContainers(t *testing.T, opt Options) [][]corev1.Container {
	db, err := BuildDistributor(opt)
	require.NoError(t, err)
	in, err := BuildIngester(opt)
	require.NoError(t, err)
	qr, err := BuildQuerier(opt)
	require.NoError(t, err)
	qf, err := BuildQueryFrontend(opt)
	require.NoError(t, err)
	cm, err := BuildCompactor(opt)
	require.NoError(t, err)
	ig, err := BuildIndexGateway(opt)
	require.NoError(t, err)
	rl, err := BuildRuler(opt)
	require.NoError(t, err)

	return [][]corev1.Container{
		db[0].(*appsv1.Deployment).Spec.Template.Spec.Containers,
		in[0].(*appsv1.StatefulSet).Spec.Template.Spec.Containers,
		qr[0].(*appsv1.Deployment).Spec.Template.Spec.Containers,
		qf[0].(*appsv1.Deployment).Spec.Template.Spec.Containers,
		cm[0].(*appsv1.StatefulSet).Spec.Template.Spec.Containers,
		ig[0].(*appsv1.StatefulSet).Spec.Template.Spec.Containers,
		rl[0].(*appsv1.StatefulSet).Spec.Template.Spec.Containers,
	}
}
