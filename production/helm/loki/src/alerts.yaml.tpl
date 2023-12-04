---
groups:
  - name: "loki_alerts"
    rules:
      - alert: "LokiRequestErrors"
        annotations:
          message: |
            {{`{{`}} $labels.job {{`}}`}} {{`{{`}} $labels.route {{`}}`}} is experiencing {{`{{`}} printf "%.2f" $value {{`}}`}}% errors.
        expr: |
          100 * sum(rate(loki_request_duration_seconds_count{status_code=~"5.."}[2m])) by (namespace, job, route)
            /
          sum(rate(loki_request_duration_seconds_count[2m])) by (namespace, job, route)
            > 10
        for: "15m"
        labels:
          severity: "critical"
{{- if .Values.monitoring.rules.additionalRuleLabels }}
{{ toYaml .Values.monitoring.rules.additionalRuleLabels | indent 10 }}
{{- end }}
      - alert: "LokiRequestPanics"
        annotations:
          message: |
            {{`{{`}} $labels.job {{`}}`}} is experiencing {{`{{`}} printf "%.2f" $value {{`}}`}}% increase of panics.
        expr: |
          sum(increase(loki_panic_total[10m])) by (namespace, job) > 0
        labels:
          severity: "critical"
{{- if .Values.monitoring.rules.additionalRuleLabels }}
{{ toYaml .Values.monitoring.rules.additionalRuleLabels | indent 10 }}
{{- end }}
      - alert: "LokiRequestLatency"
        annotations:
          message: |
            {{`{{`}} $labels.job {{`}}`}} {{`{{`}} $labels.route {{`}}`}} is experiencing {{`{{`}} printf "%.2f" $value {{`}}`}}s 99th percentile latency.
        expr: |
          namespace_job_route:loki_request_duration_seconds:99quantile{route!~"(?i).*tail.*"} > 1
        for: "15m"
        labels:
          severity: "critical"
{{- if .Values.monitoring.rules.additionalRuleLabels }}
{{ toYaml .Values.monitoring.rules.additionalRuleLabels | indent 10 }}
{{- end }}
      - alert: "LokiTooManyCompactorsRunning"
        annotations:
          message: |
            {{`{{`}} $labels.cluster {{`}}`}} {{`{{`}} $labels.namespace {{`}}`}} has had {{`{{`}} printf "%.0f" $value {{`}}`}} compactors running for more than 5m. Only one compactor should run at a time.
        expr: |
          sum(loki_boltdb_shipper_compactor_running) by (namespace, cluster) > 1
        for: "5m"
        labels:
          severity: "warning"
{{- if .Values.monitoring.rules.additionalRuleLabels }}
{{ toYaml .Values.monitoring.rules.additionalRuleLabels | indent 10 }}
{{- end }}
  - name: "loki_canaries_alerts"
    rules:
      - alert: "LokiCanaryLatency"
        annotations:
          message: |
            {{`{{`}} $labels.job {{`}}`}} is experiencing {{`{{`}} printf "%.2f" $value {{`}}`}}s 99th percentile latency.
        expr: |
          histogram_quantile(0.99, sum(rate(loki_canary_response_latency_seconds_bucket[5m])) by (le, namespace, job)) > 5
        for: "15m"
        labels:
          severity: "warning"
{{- if .Values.monitoring.rules.additionalRuleLabels }}
{{ toYaml .Values.monitoring.rules.additionalRuleLabels | indent 10 }}
{{- end }}
