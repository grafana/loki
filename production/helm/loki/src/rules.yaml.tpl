---
groups:
  - name: "loki_rules"
    rules:
    - expr: histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[1m]))
        by (le, job))
      record: job:loki_request_duration_seconds:99quantile
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: histogram_quantile(0.50, sum(rate(loki_request_duration_seconds_bucket[1m]))
        by (le, job))
      record: job:loki_request_duration_seconds:50quantile
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_sum[1m])) by (job) / sum(rate(loki_request_duration_seconds_count[1m]))
        by (job)
      record: job:loki_request_duration_seconds:avg
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job)
      record: job:loki_request_duration_seconds_bucket:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_sum[1m])) by (job)
      record: job:loki_request_duration_seconds_sum:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_count[1m])) by (job)
      record: job:loki_request_duration_seconds_count:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[1m]))
        by (le, job, route))
      record: job_route:loki_request_duration_seconds:99quantile
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: histogram_quantile(0.50, sum(rate(loki_request_duration_seconds_bucket[1m]))
        by (le, job, route))
      record: job_route:loki_request_duration_seconds:50quantile
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_sum[1m])) by (job, route) / sum(rate(loki_request_duration_seconds_count[1m]))
        by (job, route)
      record: job_route:loki_request_duration_seconds:avg
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job, route)
      record: job_route:loki_request_duration_seconds_bucket:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_sum[1m])) by (job, route)
      record: job_route:loki_request_duration_seconds_sum:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_count[1m])) by (job, route)
      record: job_route:loki_request_duration_seconds_count:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[1m]))
        by (le, namespace, job, route))
      record: namespace_job_route:loki_request_duration_seconds:99quantile
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: histogram_quantile(0.50, sum(rate(loki_request_duration_seconds_bucket[1m]))
        by (le, namespace, job, route))
      record: namespace_job_route:loki_request_duration_seconds:50quantile
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_sum[1m])) by (namespace, job, route)
        / sum(rate(loki_request_duration_seconds_count[1m])) by (namespace, job, route)
      record: namespace_job_route:loki_request_duration_seconds:avg
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, namespace, job,
        route)
      record: namespace_job_route:loki_request_duration_seconds_bucket:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_sum[1m])) by (namespace, job, route)
      record: namespace_job_route:loki_request_duration_seconds_sum:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
    - expr: sum(rate(loki_request_duration_seconds_count[1m])) by (namespace, job, route)
      record: namespace_job_route:loki_request_duration_seconds_count:sum_rate
      labels:
        cluster: "{{ include "loki.clusterLabel" $ }}"
