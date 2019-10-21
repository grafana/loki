{
  dashboards+: {
    'loki-logs.json': {
        "annotations": {
            "list": [
            {
                "builtIn": 1,
                "datasource": "-- Grafana --",
                "enable": true,
                "hide": true,
                "iconColor": "rgba(0, 211, 255, 1)",
                "name": "Annotations & Alerts",
                "type": "dashboard"
            }
            ]
        },
        "editable": true,
        "gnetId": null,
        "graphTooltip": 1,
        "id": 35,
        "iteration": 1571671540701,
        "rows": [],
        "links": [],
        "panels": [
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 0,
                "y": 0
            },
            "id": 35,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(go_goroutines{cluster=\"$cluster\", namespace=\"$namespace\", instance=~\"$deployment.*\", instance=~\"$pod\"})",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "goroutines",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 3,
                "y": 0
            },
            "id": 41,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(go_gc_duration_seconds{cluster=\"$cluster\", namespace=\"$namespace\", instance=~\"$deployment.*\", instance=~\"$pod\"}) by (quantile)",
                "legendFormat": "{{quantile}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "gc duration",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 6,
                "y": 0
            },
            "id": 36,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(rate(container_cpu_usage_seconds_total{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"$deployment.*\", pod_name=~\"$pod\", container_name=~\"$container\"}[5m]))",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "cpu",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 9,
                "y": 0
            },
            "id": 40,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(container_memory_working_set_bytes{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"$deployment.*\", pod_name=~\"$pod\", container_name=~\"$container\"})",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "memory",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "bytes",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 12,
                "y": 0
            },
            "id": 38,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(rate(container_network_transmit_bytes_total{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"$deployment.*\", pod_name=~\"$pod\"}[5m]))",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "tx",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "bytes",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 15,
                "y": 0
            },
            "id": 39,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(rate(container_network_receive_bytes_total{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"$deployment.*\", pod_name=~\"$pod\"}[5m]))",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "rx",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "decbytes",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 18,
                "y": 0
            },
            "id": 37,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "increase(kube_pod_container_status_last_terminated_reason{cluster=\"$cluster\", namespace=\"$namespace\", pod=~\"$deployment.*\", pod=~\"$pod\", container=~\"$container\"}[30m]) > 0",
                "legendFormat": "{{reason}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "restarts",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "bytes",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$metrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 4,
                "w": 3,
                "x": 21,
                "y": 0
            },
            "id": 42,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": true,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [],
            "spaceLength": 10,
            "stack": false,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(rate(promtail_custom_bad_words_total{cluster=\"$cluster\", exported_namespace=\"$namespace\", exported_instance=~\"$deployment.*\", exported_instance=~\"$pod\", container_name=~\"$container\"}[5m])) by (level)",
                "legendFormat": "{{level}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "bad words",
            "tooltip": {
                "shared": true,
                "sort": 0,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": true,
                "values": []
            },
            "yaxes": [
                {
                "format": "bytes",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "aliasColors": {},
            "bars": true,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$logmetrics",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 2,
                "w": 24,
                "x": 0,
                "y": 4
            },
            "id": 31,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": false,
                "total": false,
                "values": false
            },
            "lines": false,
            "linewidth": 1,
            "nullPointMode": "null",
            "options": {
                "dataLinks": []
            },
            "percentage": false,
            "pointradius": 2,
            "points": false,
            "renderer": "flot",
            "seriesOverrides": [
                {
                "alias": "warn",
                "color": "#FF780A"
                },
                {
                "alias": "error",
                "color": "#E02F44"
                },
                {
                "alias": "info",
                "color": "#56A64B"
                },
                {
                "alias": "debug",
                "color": "#3274D9"
                }
            ],
            "spaceLength": 10,
            "stack": true,
            "steppedLine": false,
            "targets": [
                {
                "expr": "sum(rate({cluster=\"$cluster\", namespace=\"$namespace\", instance=~\"$deployment.*\", instance=~\"$pod\", container_name=~\"$container\", level=~\"$level\"}$filter[5m])) by (level)",
                "intervalFactor": 3,
                "legendFormat": "{{level}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "",
            "tooltip": {
                "shared": true,
                "sort": 2,
                "value_type": "individual"
            },
            "type": "graph",
            "xaxis": {
                "buckets": null,
                "mode": "time",
                "name": null,
                "show": false,
                "values": []
            },
            "yaxes": [
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": true
                },
                {
                "format": "short",
                "label": null,
                "logBase": 1,
                "max": null,
                "min": null,
                "show": false
                }
            ],
            "yaxis": {
                "align": false,
                "alignLevel": null
            }
            },
            {
            "datasource": "$logs",
            "gridPos": {
                "h": 19,
                "w": 24,
                "x": 0,
                "y": 6
            },
            "id": 29,
            "maxDataPoints": "",
            "options": {
                "showTime": true,
                "sortOrder": "Descending"
            },
            "targets": [
                {
                "expr": "{cluster=\"$cluster\", namespace=\"$namespace\", instance=~\"$deployment.*\", instance=~\"$pod\", container_name=~\"$container\", level=~\"$level\"} $filter",
                "refId": "A"
                }
            ],
            "timeFrom": null,
            "timeShift": null,
            "title": "Logs",
            "type": "logs"
            }
        ],
        "refresh": false,
        "schemaVersion": 20,
        "style": "dark",
        "tags": [],
        "templating": {
            "list": [
            {
                "current": {
                "text": "Loki-ops",
                "value": "Loki-ops"
                },
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "logs",
                "options": [],
                "query": "loki",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "type": "datasource"
            },
            {
                "current": {
                "text": "Loki-Prometheus",
                "value": "Loki-Prometheus"
                },
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "logmetrics",
                "options": [],
                "query": "prometheus",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "type": "datasource"
            },
            {
                "current": {
                "tags": [],
                "text": "ops-tools1",
                "value": "ops-tools1"
                },
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "metrics",
                "options": [],
                "query": "prometheus",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "type": "datasource"
            },
            {
                "allValue": null,
                "current": {
                "text": "ops-tools1",
                "value": "ops-tools1"
                },
                "datasource": "ops-tools1",
                "definition": "label_values(kube_pod_container_info, cluster)",
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "cluster",
                "options": [],
                "query": "label_values(kube_pod_container_info, cluster)",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "sort": 0,
                "tagValuesQuery": "",
                "tags": [],
                "tagsQuery": "",
                "type": "query",
                "useTags": false
            },
            {
                "allValue": null,
                "current": {
                "text": "tempo-dev",
                "value": "tempo-dev"
                },
                "datasource": "ops-tools1",
                "definition": "label_values(kube_pod_container_info{cluster=\"$cluster\"}, namespace)",
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "namespace",
                "options": [],
                "query": "label_values(kube_pod_container_info{cluster=\"$cluster\"}, namespace)",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "sort": 0,
                "tagValuesQuery": "",
                "tags": [],
                "tagsQuery": "",
                "type": "query",
                "useTags": false
            },
            {
                "allValue": ".*",
                "current": {
                "text": "distributor",
                "value": "distributor"
                },
                "datasource": "ops-tools1",
                "definition": "label_values(kube_deployment_created{cluster=\"$cluster\", namespace=\"$namespace\"}, deployment)",
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "deployment",
                "options": [],
                "query": "label_values(kube_deployment_created{cluster=\"$cluster\", namespace=\"$namespace\"}, deployment)",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "sort": 0,
                "tagValuesQuery": "",
                "tags": [],
                "tagsQuery": "",
                "type": "query",
                "useTags": false
            },
            {
                "allValue": ".*",
                "current": {
                "text": "All",
                "value": "$__all"
                },
                "datasource": "ops-tools1",
                "definition": "label_values(kube_pod_container_info{cluster=\"$cluster\", namespace=\"$namespace\", pod=~\"$deployment.*\"}, pod)",
                "hide": 0,
                "includeAll": true,
                "label": null,
                "multi": false,
                "name": "pod",
                "options": [],
                "query": "label_values(kube_pod_container_info{cluster=\"$cluster\", namespace=\"$namespace\", pod=~\"$deployment.*\"}, pod)",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "sort": 0,
                "tagValuesQuery": "",
                "tags": [],
                "tagsQuery": "",
                "type": "query",
                "useTags": false
            },
            {
                "allValue": ".*",
                "current": {
                "text": "All",
                "value": "$__all"
                },
                "datasource": "ops-tools1",
                "definition": "label_values(kube_pod_container_info{cluster=\"$cluster\", namespace=\"$namespace\", pod=~\"$pod\", pod=~\"$deployment.*\"}, container)",
                "hide": 0,
                "includeAll": true,
                "label": null,
                "multi": false,
                "name": "container",
                "options": [],
                "query": "label_values(kube_pod_container_info{cluster=\"$cluster\", namespace=\"$namespace\", pod=~\"$pod\", pod=~\"$deployment.*\"}, container)",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "sort": 0,
                "tagValuesQuery": "",
                "tags": [],
                "tagsQuery": "",
                "type": "query",
                "useTags": false
            },
            {
                "allValue": ".*",
                "current": {
                "text": "All",
                "value": [
                    "$__all"
                ]
                },
                "hide": 0,
                "includeAll": true,
                "label": null,
                "multi": true,
                "name": "level",
                "options": [
                {
                    "selected": true,
                    "text": "All",
                    "value": "$__all"
                },
                {
                    "selected": false,
                    "text": "debug",
                    "value": "debug"
                },
                {
                    "selected": false,
                    "text": "info",
                    "value": "info"
                },
                {
                    "selected": false,
                    "text": "warn",
                    "value": "warn"
                },
                {
                    "selected": false,
                    "text": "error",
                    "value": "error"
                }
                ],
                "query": "debug,info,warn,error",
                "skipUrlSync": false,
                "type": "custom"
            },
            {
                "current": {
                "text": "",
                "value": ""
                },
                "hide": 0,
                "label": "LogQL Filter",
                "name": "filter",
                "options": [
                {
                    "selected": true,
                    "text": "",
                    "value": ""
                }
                ],
                "query": "",
                "skipUrlSync": false,
                "type": "textbox"
            }
            ]
        },
        "time": {
            "from": "now-12h",
            "to": "now"
        },
        "timepicker": {
            "refresh_intervals": [
            "5s",
            "10s",
            "30s",
            "1m",
            "5m",
            "15m",
            "30m",
            "1h",
            "2h",
            "1d"
            ]
        },
        "timezone": "",
        "title": "Logs",
        "uid": "XulAp2oZz",
        "version": 3
        }
  },
}
