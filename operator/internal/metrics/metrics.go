package metrics

import (
	"reflect"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
)

// UserDefinedLimitsType defines a label that describes the type of limits
// imposed on the cluster
type UserDefinedLimitsType string

const (
	labelGlobal UserDefinedLimitsType = "global"
	labelTenant UserDefinedLimitsType = "tenant"
)

var (
	deploymentMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lokistack_deployments",
			Help: "Number of clusters that are deployed",
		},
		[]string{"size", "stack_id"},
	)

	userDefinedLimitsMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lokistack_user_defined_limits",
			Help: "Number of clusters that are using user defined limits",
		},
		[]string{"size", "stack_id", "type"},
	)

	globalStreamLimitMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lokistack_global_stream_limit",
			Help: "Sum of stream limits used globally by the ingesters",
		},
		[]string{"size", "stack_id"},
	)

	averageTenantStreamLimitMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lokistack_avg_stream_limit_per_tenant",
			Help: "Sum of stream limits used for defined tenants by the ingesters",
		},
		[]string{"size", "stack_id"},
	)
)

// RegisterMetricCollectors registers the prometheus collectors with the k8 default metrics
func RegisterMetricCollectors() {
	metricCollectors := []prometheus.Collector{
		deploymentMetric,
		userDefinedLimitsMetric,
		globalStreamLimitMetric,
		averageTenantStreamLimitMetric,
	}

	for _, collector := range metricCollectors {
		metrics.Registry.MustRegister(collector)
	}
}

// Collect takes metrics based on the spec
func Collect(spec *lokiv1.LokiStackSpec, stackName string) {
	defaultSpec := manifests.DefaultLokiStackSpec(spec.Size)
	sizes := []lokiv1.LokiStackSizeType{lokiv1.SizeOneXSmall, lokiv1.SizeOneXMedium}

	for _, size := range sizes {
		var (
			globalRate                float64
			tenantRate                float64
			isUsingSize               = false
			isUsingTenantLimits       = false
			isUsingCustomGlobalLimits = false
		)

		if spec.Size == size {
			isUsingSize = true

			if !reflect.DeepEqual(spec.Limits.Global, defaultSpec.Limits.Global) {
				isUsingCustomGlobalLimits = true
			}

			if len(spec.Limits.Tenants) != 0 {
				isUsingTenantLimits = true
			}

			if ingesters := spec.Template.Ingester.Replicas; ingesters > 0 {
				tenantRate = streamRate(spec.Limits.Tenants, ingesters)
				globalRate = float64(spec.Limits.Global.IngestionLimits.MaxGlobalStreamsPerTenant / ingesters)
			}
		}

		setDeploymentMetric(size, stackName, isUsingSize)
		setUserDefinedLimitsMetric(size, stackName, labelGlobal, isUsingCustomGlobalLimits)
		setUserDefinedLimitsMetric(size, stackName, labelTenant, isUsingTenantLimits)
		setGlobalStreamLimitMetric(size, stackName, globalRate)
		setAverageTenantStreamLimitMetric(size, stackName, tenantRate)
	}
}

func setDeploymentMetric(size lokiv1.LokiStackSizeType, identifier string, active bool) {
	deploymentMetric.With(prometheus.Labels{
		"size":     string(size),
		"stack_id": identifier,
	}).Set(boolValue(active))
}

func setUserDefinedLimitsMetric(size lokiv1.LokiStackSizeType, identifier string, limitType UserDefinedLimitsType, active bool) {
	userDefinedLimitsMetric.With(prometheus.Labels{
		"size":     string(size),
		"stack_id": identifier,
		"type":     string(limitType),
	}).Set(boolValue(active))
}

func setGlobalStreamLimitMetric(size lokiv1.LokiStackSizeType, identifier string, rate float64) {
	globalStreamLimitMetric.With(prometheus.Labels{
		"size":     string(size),
		"stack_id": identifier,
	}).Set(rate)
}

func setAverageTenantStreamLimitMetric(size lokiv1.LokiStackSizeType, identifier string, rate float64) {
	averageTenantStreamLimitMetric.With(prometheus.Labels{
		"size":     string(size),
		"stack_id": identifier,
	}).Set(rate)
}

func boolValue(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func streamRate(tenantLimits map[string]lokiv1.LimitsTemplateSpec, ingesters int32) float64 {
	var tenants, tenantStreamLimit int32 = 0, 0

	for _, tenant := range tenantLimits {
		if tenant.IngestionLimits == nil || tenant.IngestionLimits.MaxGlobalStreamsPerTenant == 0 {
			continue
		}

		tenants++
		tenantStreamLimit += tenant.IngestionLimits.MaxGlobalStreamsPerTenant
	}

	if tenants == 0 || ingesters == 0 {
		return 0
	}
	return float64(tenantStreamLimit / ingesters / tenants)
}
