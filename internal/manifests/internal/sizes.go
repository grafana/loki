package internal

import (
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ResourceSizes is a map of component->requests/limits
type ResourceSizes struct {
	Querier       corev1.ResourceRequirements
	Ingester      corev1.ResourceRequirements
	Distributor   corev1.ResourceRequirements
	QueryFrontend corev1.ResourceRequirements
	Compactor     corev1.ResourceRequirements
}

// ResourceSizeTable defines the default resource requests and limits for each size
var ResourceSizeTable = map[lokiv1beta1.LokiStackSizeType]ResourceSizes{
	lokiv1beta1.SizeOneXExtraSmall: {
		Querier: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
		},
		Ingester: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		Distributor: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		QueryFrontend: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		Compactor: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	},
	lokiv1beta1.SizeOneXSmall: {
		Querier: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		Ingester: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("20Gi"),
			},
		},
		Distributor: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		QueryFrontend: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("2.5Gi"),
			},
		},
		Compactor: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
	},
	lokiv1beta1.SizeOneXMedium: {
		Querier: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("6"),
				corev1.ResourceMemory: resource.MustParse("10Gi"),
			},
		},
		Ingester: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("6"),
				corev1.ResourceMemory: resource.MustParse("30Gi"),
			},
		},
		Distributor: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		QueryFrontend: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("2.5Gi"),
			},
		},
		Compactor: corev1.ResourceRequirements{
			Limits: nil,
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
	},
}

// StackSizeTable defines the default configurations for each size
var StackSizeTable = map[lokiv1beta1.LokiStackSizeType]lokiv1beta1.LokiStackSpec{

	lokiv1beta1.SizeOneXExtraSmall: {
		Size:              lokiv1beta1.SizeOneXExtraSmall,
		ReplicationFactor: 1,
		Limits: lokiv1beta1.LimitsSpec{
			Global: lokiv1beta1.LimitsTemplateSpec{
				IngestionLimits: lokiv1beta1.IngestionLimitSpec{
					IngestionRate:      20,
					IngestionBurstSize: 10,
					MaxStreamsPerUser:  25000,
				},
				QueryLimits: lokiv1beta1.QueryLimitSpec{
					MaxEntriesLimitPerQuery: 0,
					MaxChunksPerQuery:       0,
					MaxQuerySeries:          0,
				},
			},
		},
		Template: lokiv1beta1.LokiTemplateSpec{
			Compactor: lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Distributor: lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Ingester: lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Querier: lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			QueryFrontend: lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
		},
	},

	lokiv1beta1.SizeOneXSmall: {
		Size:              lokiv1beta1.SizeOneXSmall,
		ReplicationFactor: 2,
		Limits: lokiv1beta1.LimitsSpec{
			Global: lokiv1beta1.LimitsTemplateSpec{
				IngestionLimits: lokiv1beta1.IngestionLimitSpec{
					IngestionRate:      20,
					IngestionBurstSize: 10,
					MaxStreamsPerUser:  25000,
				},
				QueryLimits: lokiv1beta1.QueryLimitSpec{
					MaxEntriesLimitPerQuery: 0,
					MaxChunksPerQuery:       0,
					MaxQuerySeries:          0,
				},
			},
		},
		Template: lokiv1beta1.LokiTemplateSpec{
			Compactor: lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Distributor: lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Ingester: lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Querier: lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			QueryFrontend: lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
		},
	},

	lokiv1beta1.SizeOneXMedium: {
		Size:              lokiv1beta1.SizeOneXMedium,
		ReplicationFactor: 3,
		Limits: lokiv1beta1.LimitsSpec{
			Global: lokiv1beta1.LimitsTemplateSpec{
				IngestionLimits: lokiv1beta1.IngestionLimitSpec{
					IngestionRate:      20,
					IngestionBurstSize: 10,
					MaxStreamsPerUser:  25000,
				},
				QueryLimits: lokiv1beta1.QueryLimitSpec{
					MaxEntriesLimitPerQuery: 0,
					MaxChunksPerQuery:       0,
					MaxQuerySeries:          0,
				},
			},
		},
		Template: lokiv1beta1.LokiTemplateSpec{
			Compactor: lokiv1beta1.LokiComponentSpec{
				Replicas: 1,
			},
			Distributor: lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
			Ingester: lokiv1beta1.LokiComponentSpec{
				Replicas: 3,
			},
			Querier: lokiv1beta1.LokiComponentSpec{
				Replicas: 3,
			},
			QueryFrontend: lokiv1beta1.LokiComponentSpec{
				Replicas: 2,
			},
		},
	},
}
