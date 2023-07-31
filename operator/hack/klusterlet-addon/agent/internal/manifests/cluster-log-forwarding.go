package manifests

import (
	loggingv1 "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func BuildClusterLogForwarder(lokiHubURL string, mTLSSecretName string) ([]client.Object, error) {
	clf := &loggingv1.ClusterLogForwarder{
		ObjectMeta: v1.ObjectMeta{
			Name:      "instance",
			Namespace: "openshift-logging",
		},

		Spec: loggingv1.ClusterLogForwarderSpec{
			Outputs: []loggingv1.OutputSpec{
				{
					Name: "spoke-infra-to-hub",
					Type: "loki",
					URL:  lokiHubURL,
					OutputTypeSpec: loggingv1.OutputTypeSpec{
						Loki: &loggingv1.Loki{
							LabelKeys: []string{
								"log_type",
								"kubernetes.namespace_name",
								"kubernetes.pod_name",
								"openshift.cluster_id",
							},
						},
					},
					Secret: &loggingv1.OutputSecretSpec{
						Name: mTLSSecretName,
					},
				},
			},
			Pipelines: []loggingv1.PipelineSpec{
				{
					Name: "send-infra-logs-to-hub",
					InputRefs: []string{
						"infrastructure",
					},
					OutputRefs: []string{
						"spoke-infra-to-hub",
					},
				},
			},
		},
	}

	return []client.Object{clf}, nil
}
