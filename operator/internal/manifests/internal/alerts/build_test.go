package alerts

import (
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/internal"

	"github.com/stretchr/testify/require"
)

func TestBuild_HappyPath(t *testing.T) {
	expAlerts := `
---
"groups":
- "name": logging_loki.alerts
  "rules":
  - "alert": LokiRequestErrors
    "annotations":
      "message": |-
        {{ $labels.job }} {{ $labels.route }} is experiencing {{ printf "%.2f" $value }}% errors.
      "summary": "At least 10% of requests are responded by 5xx server errors."
      "runbook_url": "TBD"
    "expr": |
      sum(
          rate(
              loki_request_duration_seconds_count{status_code=~"5.."}[1m]
          )
      ) by (namespace, job, route)
      /
      sum(
          rate(
              loki_request_duration_seconds_count[1m]
          )
      ) by (namespace, job, route)
      * 100
      > 10
    "for": 15m
    "labels":
      "severity": critical
  - "alert": LokiRequestPanics
    "annotations":
      "message": |-
        {{ $labels.job }} is experiencing an increase of {{ $value }} panics.
      "summary": "A panic was triggered."
      "runbook_url": "TBD"
    expr: |
      sum(
          increase(
              loki_panic_total[10m]
          )
      ) by (namespace, job)
      > 0
    "labels":
        "severity": critical
  - "alert": LokiRequestLatency
    "annotations":
      "message": |-
        {{ $labels.job }} {{ $labels.route }} is experiencing {{ printf "%.2f" $value }}s 99th percentile latency.
      "summary": "The 99th percentile is experiencing high latency (higher than 1 second)."
      "runbook_url": "TBD"
    expr: |
      namespace_job_route:loki_request_duration_seconds:99quantile{route!~"(?i).*tail.*"}
      > 1
    "for": 15m
    "labels":
      "severity": critical
  - "alert": LokiTenantRateLimit
    "annotations":
      "message": |-
        {{ $labels.tenant }} is experiencing rate limiting for reason '{{ $labels.reason }}': {{ printf "%.0f" $value }}
      "summary": "A tenant is experiencing rate limiting. The number of discarded logs per tenant and reason over the last 30 minutes is above 100."
      "runbook_url": "TBD"
    expr: |
      sum (
        increase(
            loki_discarded_samples_total{}[30m]
        )
      ) by (namespace, tenant, reason)
      > 100
    "for": 15m
    "labels":
      "severity": warning
  - "alert": LokiCPUHigh
    "annotations":
      "message": |-
        {{ $labels.container }} CPU usage is high: {{ $value }}%
      "summary": "Loki's CPU usage is high (above 75% for 5 minutes)"
      "runbook_url": "TBD"
    expr: |
      sum(
          rate(
              container_cpu_usage_seconds_total{container=~"loki-.*"}[5m]
          )
      ) by (namespace, container)
      /
      sum(
          kube_pod_container_resource_limits{resource="cpu"}
      ) by (namespace, container)
      * 100
      > 75
    "for": 5m
    "labels":
      "severity": warning
  - "alert": LokiCPUHigh
    "annotations":
      "message": |-
        {{ $labels.container }} CPU usage is high: {{ $value }}%
      "summary": "Loki's CPU usage is high (above 95% for 5 minutes)"
      "runbook_url": "TBD"
    expr: |
      sum(
          rate(
              container_cpu_usage_seconds_total{container=~"loki-.*"}[5m]
          )
      ) by (namespace, container)
      /
      sum(
          kube_pod_container_resource_limits{resource="cpu"}
      ) by (namespace, container)
      * 100
      > 95
    "for": 5m
    "labels":
      "severity": critical
  - "alert": LokiMemoryHigh
    "annotations":
      "message": |-
        {{ $labels.container }} memory usage is high: {{ $value }}%
      "summary": "Loki's memory usage is high (above 75% of the configured limit for 5 minutes)"
      "runbook_url": "TBD"
    expr: |
      sum(
        container_memory_working_set_bytes{container=~"loki-.*"}
      ) by (namespace, container)
      /
      sum(
        kube_pod_container_resource_limits{resource="memory"}
      ) by (namespace, container)
      * 100
      > 75
    "for": 5m
    "labels":
      "severity": warning
  - "alert": LokiMemoryHigh
    "annotations":
      "message": |-
        {{ $labels.container }} memory usage is high: {{ $value }}%
      "summary": "Loki's memory usage is high (above 95% of the configured limit for 5 minutes)"
      "runbook_url": "TBD"
    expr: |
      sum(
        container_memory_working_set_bytes{container=~"loki-.*"}
      ) by (namespace, container)
      /
      sum(
        kube_pod_container_resource_limits{resource="memory"}
      ) by (namespace, container)
      * 100
      > 95
    "for": 5m
    "labels":
      "severity": critical
  - "alert": LokiStorageSlowWrite
    "annotations":
      "message": "The 99th percentile ({{ $value }}s) is experiencing slow write"
      "summary": "The 99th percentile ({{ $value }}s) is experiencing slow write"
      "runbook_url": "TBD"
    expr: |
      histogram_quantile(.99,
          sum(
              rate(
                  loki_boltdb_shipper_request_duration_seconds_bucket{operation="WRITE"}[5m]
              )
          ) by (le, namespace, operation)
      )
      > 1
    "for": 5m
    "labels":
      "severity": warning
  - "alert": LokiStorageSlowRead
    "annotations":
      "message": "The 99th percentile ({{ $value }}s) is experiencing slow read"
      "summary": "The 99th percentile ({{ $value }}s) is experiencing slow read"
      "runbook_url": "TBD"
    expr: |
      histogram_quantile(.99,
          sum(
              rate(
                  loki_boltdb_shipper_request_duration_seconds_bucket{operation="QUERY"}[5m]
              )
          ) by (le, namespace, operation)
      )
      > 1
    "for": 5m
    "labels":
      "severity": warning
  - "alert": LokiWritePathHighLoad
    "annotations":
      "message": |-
        The write path of Loki is loaded: {{ printf "%0.f" $value }} B/s
      "summary": "The write path of Loki is loaded"
      "runbook_url": "TBD"
    expr: |
      sum(
          rate(
              loki_distributor_bytes_received_total{}[5m]
          )
      ) by (namespace)
      >
      1e+06
    "for": 5m
    "labels":
      "severity": warning
  - "alert": LokiReadPathHighLoad
    "annotations":
      "message": "The read path of Loki is loaded: {{ $value }} QPS"
      "summary": "The read path of Loki is loaded"
      "runbook_url": "TBD"
    # HTTP & GRPC routes
    expr: |
      sum(
          rate(
              loki_request_duration_seconds_count{
                  job="query-frontend",
                  route=~"loki_api_v1_series|api_prom_series|api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_labels|loki_api_v1_label_name_values|/logproto.Querier/Query|/logproto.Querier/Label|/logproto.Querier/Series|/logproto.Querier/QuerySample|/logproto.Querier/GetChunkIDs",
              }[5m]
          )
      ) by (namespace)
      >
      1
    "for": 5m
    "labels":
      "severity": warning
`
	expRules := `
---
"groups":
- "name": logging_loki.rules
  "rules":
  - record: namespace_job_route:loki_request_duration_seconds:99quantile
    "expr": |
      histogram_quantile(0.99,
          sum(
              rate(
                  loki_request_duration_seconds_bucket[1m]
              )
          )
          by (le, namespace, job, route)
      )

`
	size := lokiv1beta1.SizeOneXExtraSmall
	opts := Options{
		Stack: lokiv1beta1.LokiStackSpec{
			Size: size,
		},
		WritePathHighLoadThreshold: internal.WritePathHighLoadThresholdTable[size],
		ReadPathHighLoadThreshold:  internal.ReadPathHighLoadThresholdTable[size],
	}
	alerts, rules, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expAlerts, string(alerts))
	require.YAMLEq(t, expRules, string(rules))
}
