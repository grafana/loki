package chunk

// Chunk functions used only in tests

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// BenchmarkLabels is a real example from Kubernetes' embedded cAdvisor metrics, lightly obfuscated
var BenchmarkLabels = labels.Labels{
	{Name: model.MetricNameLabel, Value: "container_cpu_usage_seconds_total"},
	{Name: "beta_kubernetes_io_arch", Value: "amd64"},
	{Name: "beta_kubernetes_io_instance_type", Value: "c3.somesize"},
	{Name: "beta_kubernetes_io_os", Value: "linux"},
	{Name: "container_name", Value: "some-name"},
	{Name: "cpu", Value: "cpu01"},
	{Name: "failure_domain_beta_kubernetes_io_region", Value: "somewhere-1"},
	{Name: "failure_domain_beta_kubernetes_io_zone", Value: "somewhere-1b"},
	{Name: "id", Value: "/kubepods/burstable/pod6e91c467-e4c5-11e7-ace3-0a97ed59c75e/a3c8498918bd6866349fed5a6f8c643b77c91836427fb6327913276ebc6bde28"},
	{Name: "image", Value: "registry/organisation/name@sha256:dca3d877a80008b45d71d7edc4fd2e44c0c8c8e7102ba5cbabec63a374d1d506"},
	{Name: "instance", Value: "ip-111-11-1-11.ec2.internal"},
	{Name: "job", Value: "kubernetes-cadvisor"},
	{Name: "kubernetes_io_hostname", Value: "ip-111-11-1-11"},
	{Name: "monitor", Value: "prod"},
	{Name: "name", Value: "k8s_some-name_some-other-name-5j8s8_kube-system_6e91c467-e4c5-11e7-ace3-0a97ed59c75e_0"},
	{Name: "namespace", Value: "kube-system"},
	{Name: "pod_name", Value: "some-other-name-5j8s8"},
}

// DefaultSchemaConfig creates a simple schema config for testing
func DefaultSchemaConfig(store, schema string, from model.Time) SchemaConfig {
	return SchemaConfig{
		Configs: []PeriodConfig{{
			IndexType: store,
			Schema:    schema,
			From:      DayTime{from},
			ChunkTables: PeriodicTableConfig{
				Prefix: "cortex",
				Period: 7 * 24 * time.Hour,
			},
			IndexTables: PeriodicTableConfig{
				Prefix: "cortex_chunks",
				Period: 7 * 24 * time.Hour,
			},
		}},
	}
}

// ChunksToMatrix converts a set of chunks to a model.Matrix.
func ChunksToMatrix(ctx context.Context, chunks []Chunk, from, through model.Time) (model.Matrix, error) {
	// Group chunks by series, sort and dedupe samples.
	metrics := map[model.Fingerprint]model.Metric{}
	samplesBySeries := map[model.Fingerprint][][]model.SamplePair{}
	for _, c := range chunks {
		ss, err := c.Samples(from, through)
		if err != nil {
			return nil, err
		}

		metrics[c.Fingerprint] = util.LabelsToMetric(c.Metric)
		samplesBySeries[c.Fingerprint] = append(samplesBySeries[c.Fingerprint], ss)
	}

	matrix := make(model.Matrix, 0, len(samplesBySeries))
	for fp, ss := range samplesBySeries {
		matrix = append(matrix, &model.SampleStream{
			Metric: metrics[fp],
			Values: util.MergeNSampleSets(ss...),
		})
	}

	return matrix, nil
}
