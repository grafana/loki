package internal

import (
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ComponentResources is a map of component->requests/limits
type ComponentResources struct {
	IndexGateway ResourceRequirements
	Ingester     ResourceRequirements
	Compactor    ResourceRequirements
	Ruler        ResourceRequirements
	WALStorage   ResourceRequirements
	// these two don't need a PVCSize
	Querier       corev1.ResourceRequirements
	Distributor   corev1.ResourceRequirements
	QueryFrontend corev1.ResourceRequirements
	Gateway       corev1.ResourceRequirements
}

// ResourceRequirements sets CPU, Memory, and PVC requirements for a component
type ResourceRequirements struct {
	Limits   corev1.ResourceList
	Requests corev1.ResourceList
	PVCSize  resource.Quantity
}

// ResourceRequirementsTable defines the default resource requests and limits for each size
var ResourceRequirementsTable = map[lokiv1beta1.LokiStackSizeType]ComponentResources{
	lokiv1beta1.SizeOneXExtraSmall: {
		Querier: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
		},
		Ruler: ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			PVCSize: resource.MustParse("10Gi"),
		},
		Ingester: ResourceRequirements{
			PVCSize: resource.MustParse("10Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		Distributor: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		QueryFrontend: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		Compactor: ResourceRequirements{
			PVCSize: resource.MustParse("10Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		Gateway: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
		},
		IndexGateway: ResourceRequirements{
			PVCSize: resource.MustParse("50Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		WALStorage: ResourceRequirements{
			PVCSize: resource.MustParse("150Gi"),
		},
	},
	lokiv1beta1.SizeOneXSmall: {
		Querier: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		Ruler: ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			PVCSize: resource.MustParse("10Gi"),
		},
		Ingester: ResourceRequirements{
			PVCSize: resource.MustParse("10Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("20Gi"),
			},
		},
		Distributor: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		QueryFrontend: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("2.5Gi"),
			},
		},
		Compactor: ResourceRequirements{
			PVCSize: resource.MustParse("10Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		Gateway: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		IndexGateway: ResourceRequirements{
			PVCSize: resource.MustParse("50Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		WALStorage: ResourceRequirements{
			PVCSize: resource.MustParse("150Gi"),
		},
	},
	lokiv1beta1.SizeOneXMedium: {
		Querier: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("6"),
				corev1.ResourceMemory: resource.MustParse("10Gi"),
			},
		},
		Ruler: ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
			PVCSize: resource.MustParse("10Gi"),
		},
		Ingester: ResourceRequirements{
			PVCSize: resource.MustParse("10Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("6"),
				corev1.ResourceMemory: resource.MustParse("30Gi"),
			},
		},
		Distributor: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		QueryFrontend: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("2.5Gi"),
			},
		},
		Compactor: ResourceRequirements{
			PVCSize: resource.MustParse("10Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		Gateway: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		IndexGateway: ResourceRequirements{
			PVCSize: resource.MustParse("50Gi"),
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		WALStorage: ResourceRequirements{
			PVCSize: resource.MustParse("150Gi"),
		},
	},
}

// StackSizeTable defines the default configurations for each size
var StackSizeTable = map[lokiv1beta1.LokiStackSizeType]lokiv1beta1.LokiStackSpec{

	lokiv1beta1.SizeOneXExtraSmall: {
		Size:              lokiv1beta1.SizeOneXExtraSmall,
		ReplicationFactor: 1,
		Limits: &lokiv1beta1.LimitsSpec{
			Global: &lokiv1beta1.LimitsTemplateSpec{
				IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
					// Defaults from Loki docs
					IngestionRate:          4,
					IngestionBurstSize:     6,
					MaxLabelNameLength:     1024,
					MaxLabelValueLength:    2048,
					MaxLabelNamesPerSeries: 30,
					MaxLineSize:            256000,
				},
				QueryLimits: &lokiv1beta1.QueryLimitSpec{
					// Defaults from Loki docs
					MaxEntriesLimitPerQuery: 5000,
					MaxChunksPerQuery:       2000000,
					MaxQuerySeries:          500,
				},
			},
		},
		Template: &lokiv1beta1.LokiTemplateSpec{
			Compactor: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Distributor: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Ingester: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Querier: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			QueryFrontend: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Gateway: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			IndexGateway: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Ruler: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
		},
	},

	lokiv1beta1.SizeOneXSmall: {
		Size:              lokiv1beta1.SizeOneXSmall,
		ReplicationFactor: 2,
		Limits: &lokiv1beta1.LimitsSpec{
			Global: &lokiv1beta1.LimitsTemplateSpec{
				IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
					// Custom for 1x.small
					IngestionRate:             10,
					IngestionBurstSize:        20,
					MaxGlobalStreamsPerTenant: 10000,
					// Defaults from Loki docs
					MaxLabelNameLength:     1024,
					MaxLabelValueLength:    2048,
					MaxLabelNamesPerSeries: 30,
					MaxLineSize:            256000,
				},
				QueryLimits: &lokiv1beta1.QueryLimitSpec{
					// Defaults from Loki docs
					MaxEntriesLimitPerQuery: 5000,
					MaxChunksPerQuery:       2000000,
					MaxQuerySeries:          500,
				},
			},
		},
		Template: &lokiv1beta1.LokiTemplateSpec{
			Compactor: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Distributor: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Ingester: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Querier: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			QueryFrontend: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Gateway: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			IndexGateway: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Ruler: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
		},
	},

	lokiv1beta1.SizeOneXMedium: {
		Size:              lokiv1beta1.SizeOneXMedium,
		ReplicationFactor: 3,
		Limits: &lokiv1beta1.LimitsSpec{
			Global: &lokiv1beta1.LimitsTemplateSpec{
				IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
					// Custom for 1x.medium
					IngestionRate:             10,
					IngestionBurstSize:        20,
					MaxGlobalStreamsPerTenant: 25000,
					// Defaults from Loki docs
					MaxLabelNameLength:     1024,
					MaxLabelValueLength:    2048,
					MaxLabelNamesPerSeries: 30,
					MaxLineSize:            256000,
				},
				QueryLimits: &lokiv1beta1.QueryLimitSpec{
					// Defaults from Loki docs
					MaxEntriesLimitPerQuery: 5000,
					MaxChunksPerQuery:       2000000,
					MaxQuerySeries:          500,
				},
			},
		},
		Template: &lokiv1beta1.LokiTemplateSpec{
			Compactor: &lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Distributor: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Ingester: &lokiv1beta1.LokiComponentSpec{
				Replicas: 3,
			},
			Querier: &lokiv1beta1.LokiComponentSpec{
				Replicas: 3,
			},
			QueryFrontend: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Gateway: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			IndexGateway: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Ruler: &lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
		},
	},
}
