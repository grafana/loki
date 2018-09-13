package chunk

import "github.com/prometheus/common/model"

// BenchmarkMetric is a real example from Kubernetes' embedded cAdvisor metrics, lightly obfuscated
var BenchmarkMetric = model.Metric{
	model.MetricNameLabel:              "container_cpu_usage_seconds_total",
	"beta_kubernetes_io_arch":          "amd64",
	"beta_kubernetes_io_instance_type": "c3.somesize",
	"beta_kubernetes_io_os":            "linux",
	"container_name":                   "some-name",
	"cpu":                              "cpu01",
	"failure_domain_beta_kubernetes_io_region": "somewhere-1",
	"failure_domain_beta_kubernetes_io_zone":   "somewhere-1b",
	"id":       "/kubepods/burstable/pod6e91c467-e4c5-11e7-ace3-0a97ed59c75e/a3c8498918bd6866349fed5a6f8c643b77c91836427fb6327913276ebc6bde28",
	"image":    "registry/organisation/name@sha256:dca3d877a80008b45d71d7edc4fd2e44c0c8c8e7102ba5cbabec63a374d1d506",
	"instance": "ip-111-11-1-11.ec2.internal",
	"job":      "kubernetes-cadvisor",
	"kubernetes_io_hostname": "ip-111-11-1-11",
	"monitor":                "prod",
	"name":                   "k8s_some-name_some-other-name-5j8s8_kube-system_6e91c467-e4c5-11e7-ace3-0a97ed59c75e_0",
	"namespace":              "kube-system",
	"pod_name":               "some-other-name-5j8s8",
}
