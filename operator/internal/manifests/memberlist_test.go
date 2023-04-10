package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

func TestConfigureHashRingEnv_UseDefaults_NoHashRingSpec(t *testing.T) {
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

	wantEnvVar := v1.EnvVar{
		ValueFrom: &v1.EnvVarSource{
			FieldRef: &v1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "status.podIP",
			},
		},
	}

	for _, cs := range lokiContainers(t, opt) {
		for _, c := range cs {
			require.NotContains(t, c.Env, wantEnvVar, "contains envVar %s for: %s", gossipInstanceAddrEnvVarName, c.Name)
		}
	}
}

func TestConfigureHashRingEnv_UseDefaults_WithCustomHashRingSpec(t *testing.T) {
	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			HashRing: &lokiv1.HashRingSpec{
				Type: lokiv1.HashRingMemberList,
				MemberList: &lokiv1.MemberListSpec{
					InstanceAddrType: lokiv1.InstanceAddrDefault,
				},
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

	wantEnvVar := v1.EnvVar{
		ValueFrom: &v1.EnvVarSource{
			FieldRef: &v1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "status.podIP",
			},
		},
	}

	for _, cs := range lokiContainers(t, opt) {
		for _, c := range cs {
			require.NotContains(t, c.Env, wantEnvVar, "contains envVar %s for: %s", gossipInstanceAddrEnvVarName, c.Name)
		}
	}
}

func TestConfigureHashRingEnv_UseInstanceAddrPodIP(t *testing.T) {
	opt := Options{
		Name:      "test",
		Namespace: "test",
		Image:     "test",
		Stack: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			HashRing: &lokiv1.HashRingSpec{
				Type: lokiv1.HashRingMemberList,
				MemberList: &lokiv1.MemberListSpec{
					InstanceAddrType: lokiv1.InstanceAddrPodIP,
				},
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

	wantEnvVar := v1.EnvVar{
		Name: gossipInstanceAddrEnvVarName,
		ValueFrom: &v1.EnvVarSource{
			FieldRef: &v1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "status.podIP",
			},
		},
	}

	for _, cs := range lokiContainers(t, opt) {
		for _, c := range cs {
			require.Contains(t, c.Env, wantEnvVar, "missing envVar %s for: %s", gossipInstanceAddrEnvVarName, c.Name)
		}
	}
}
