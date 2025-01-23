package otlp

var (
	RequiredAttributes = []string{
		"k8s.namespace.name",
		"kubernetes.namespace_name",
		"log_source",
		"log_type",
		"openshift.cluster.uid",
		"openshift.log.source",
		"openshift.log.type",
	}

	RecommendedAttributes = []string{
		"k8s.container.name",
		"k8s.cronjob.name",
		"k8s.daemonset.name",
		"k8s.deployment.name",
		"k8s.job.name",
		"k8s.node.name",
		"k8s.pod.name",
		"k8s.statefulset.name",
		"kubernetes.container_name",
		"kubernetes.host",
		"kubernetes.pod_name",
		"service.name",
	}
)
