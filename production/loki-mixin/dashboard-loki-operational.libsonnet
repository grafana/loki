{
  dashboards+: {
    'loki-operational.json': {
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
            },
            {
                "datasource": "$logs",
                "enable": true,
                "expr": "{cluster=\"$cluster\", diff_namespace=\"$namespace\", container_name=\"kube-diff-logger\"}",
                "hide": true,
                "iconColor": "rgba(255, 96, 96, 1)",
                "name": "deployments",
                "showIn": 0,
                "target": {}
            }
            ]
        },
        "editable": true,
        "gnetId": null,
        "graphTooltip": 1,
        "id": 37,
        "iteration": 1572381524593,
        "rows": [],
        "links": [
            {
            "icon": "dashboard",
            "includeVars": true,
            "keepTime": true,
            "tags": [],
            "targetBlank": true,
            "title": "Chunks",
            "tooltip": "",
            "type": "link",
            "url": "/grafana/d/586c2916e59d3f81282a776dbe8333e9/loki-chunks"
            },
            {
            "icon": "dashboard",
            "includeVars": true,
            "keepTime": true,
            "tags": [],
            "targetBlank": true,
            "title": "Memcached",
            "tooltip": "",
            "type": "link",
            "url": "/grafana/d/8127d7f60e3a3230d7c633c8b5ddf68c/memcached"
            },
            {
            "icon": "dashboard",
            "includeVars": false,
            "keepTime": true,
            "tags": [],
            "targetBlank": true,
            "title": "Promtail",
            "type": "link",
            "url": "/grafana/d/739c15a23c04c6ace0d03f6e14ba26e8/loki-promtail?var-datasource=$datasource&var-cluster=$cluster&var-namespace=default"
            },
            {
            "icon": "dashboard",
            "includeVars": true,
            "keepTime": true,
            "tags": [],
            "targetBlank": true,
            "title": "BigTable Backup",
            "type": "link",
            "url": "/grafana/d/da5ce918801d6d9f775a8c7e9f9f339a/loki-bigtable-backup"
            },
            {
            "icon": "dashboard",
            "keepTime": true,
            "tags": [],
            "targetBlank": true,
            "title": "Consul",
            "type": "link",
            "url": "/grafana/d/836793349c8ce200baa98f045123d19b/consul?var-datasource=$datasource&var-job=$namespace%2Fconsul"
            }
        ],
        "panels": [
            {
            "collapsed": false,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 0
            },
            "id": 17,
            "panels": [],
            "title": "Main",
            "type": "row"
            },
            {
            "aliasColors": {
                "5xx": "red"
            },
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 4,
                "x": 0,
                "y": 1
            },
            "id": 6,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": true,
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
                "expr": "sum by (status) (\nlabel_replace(\n  label_replace(\n        rate(loki_request_duration_seconds_count{cluster=\"$cluster\", job=\"$namespace/cortex-gw\", route=~\"api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values\"}[5m]),\n  \"status\", \"${1}xx\", \"status_code\", \"([0-9])..\"),\n\"status\", \"${1}\", \"status_code\", \"([a-z]+)\")\n)",
                "legendFormat": "{{status}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Queries/Second",
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
                "logBase": 10,
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
            "aliasColors": {
                "5xx": "red"
            },
            "bars": false,
            "dashLength": 10,
            "dashes": false,
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 4,
                "x": 4,
                "y": 1
            },
            "id": 7,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": true,
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
                "expr": "sum by (status) (\nlabel_replace(\n  label_replace(\n          rate(loki_request_duration_seconds_count{cluster=\"$cluster\", job=\"$namespace/cortex-gw\", route=~\"api_prom_push|loki_api_v1_push\"}[5m]),\n   \"status\", \"${1}xx\", \"status_code\", \"([0-9])..\"),\n\"status\", \"${1}\", \"status_code\", \"([a-z]+)\"))",
                "legendFormat": "{{status}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Pushes/Second",
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
                "logBase": 10,
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 4,
                "x": 8,
                "y": 1
            },
            "id": 11,
            "legend": {
                "avg": false,
                "current": false,
                "hideEmpty": false,
                "hideZero": false,
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
                "expr": "topk(5, sum by (name,level) (rate(promtail_custom_bad_words_total{cluster=\"$cluster\", exported_namespace=\"$namespace\"}[$__interval])) - \nsum by (name,level) (rate(promtail_custom_bad_words_total{cluster=\"$cluster\", exported_namespace=\"$namespace\"}[$__interval] offset 1h)))",
                "legendFormat": "{{name}}-{{level}}",
                "refId": "A"
              }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Bad Words",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 4,
                "x": 12,
                "y": 1
            },
            "id": 2,
            "interval": "",
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": true,
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
                "expr": "topk(10, sum(rate(loki_distributor_lines_received_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (tenant))",
                "legendFormat": "{{tenant}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Lines Per Tenant (top 10)",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 4,
                "x": 16,
                "y": 1
            },
            "id": 4,
            "legend": {
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "show": true,
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
                "expr": "topk(10, sum(rate(loki_distributor_bytes_received_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (tenant)) / 1024 / 1024",
                "legendFormat": "{{tenant}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "MBs Per Tenant (Top 10)",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 4,
                "x": 20,
                "y": 1
            },
            "id": 24,
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
                "expr": "increase(kube_pod_container_status_last_terminated_reason{cluster=\"$cluster\", namespace=\"$namespace\", reason!=\"Completed\"}[30m]) > 0",
                "legendFormat": "{{container}}-{{pod}}-{{reason}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Container Termination",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 10,
                "w": 12,
                "x": 0,
                "y": 6
            },
            "id": 9,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
                "show": true,
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
                "expr": "histogram_quantile(0.99, sum by (le) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/cortex-gw\", route=~\"api_prom_push|loki_api_v1_push\", cluster=~\"$cluster\"})) * 1e3",
                "legendFormat": ".99",
                "refId": "A"
                },
                {
                "expr": "histogram_quantile(0.75, sum by (le) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/cortex-gw\", route=~\"api_prom_push|loki_api_v1_push\", cluster=~\"$cluster\"})) * 1e3",
                "legendFormat": ".9",
                "refId": "B"
                },
                {
                "expr": "histogram_quantile(0.5, sum by (le) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/cortex-gw\", route=~\"api_prom_push|loki_api_v1_push\", cluster=~\"$cluster\"})) * 1e3",
                "legendFormat": ".5",
                "refId": "C"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Push Latency",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 12,
                "y": 6
            },
            "id": 12,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "histogram_quantile(0.99, sum by (le) (job:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/distributor\", cluster=~\"$cluster\"})) * 1e3",
                "legendFormat": ".99",
                "refId": "A"
                },
                {
                "expr": "histogram_quantile(0.9, sum by (le) (job:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/distributor\", cluster=~\"$cluster\"})) * 1e3",
                "legendFormat": ".9",
                "refId": "B"
                },
                {
                "expr": "histogram_quantile(0.5, sum by (le) (job:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/distributor\", cluster=~\"$cluster\"})) * 1e3",
                "legendFormat": ".5",
                "refId": "C"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Distributor Latency",
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
            "datasource": "$datasource",
            "fill": 0,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 18,
                "y": 6
            },
            "id": 71,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "sum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/distributor\", status_code=~\"success|200\"}[5m])) by (route)\n/\nsum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/distributor\"}[5m])) by (route)",
                "legendFormat": "{{route}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Distributor Success Rate",
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
                "decimals": null,
                "format": "percentunit",
                "label": "",
                "logBase": 1,
                "max": "1",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 12,
                "y": 11
            },
            "id": 13,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "histogram_quantile(0.99, sum by (le) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/ingester\", route=\"/logproto.Pusher/Push\", cluster=~\"$cluster\"})) * 1e3",
                "legendFormat": ".99",
                "refId": "A"
                },
                {
                "expr": "histogram_quantile(0.9, sum by (le) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/ingester\", route=\"/logproto.Pusher/Push\", cluster=~\"$cluster\"})) * 1e3",
                "hide": false,
                "legendFormat": ".9",
                "refId": "B"
                },
                {
                "expr": "histogram_quantile(0.5, sum by (le) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=~\"($namespace)/ingester\", route=\"/logproto.Pusher/Push\", cluster=~\"$cluster\"})) * 1e3",
                "hide": false,
                "legendFormat": ".5",
                "refId": "C"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Ingester Latency",
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
            "datasource": "$datasource",
            "fill": 0,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 18,
                "y": 11
            },
            "id": 72,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "sum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\", status_code=~\"success|200\", route=\"/logproto.Pusher/Push\"}[5m])) by (route)\n/\nsum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\", route=\"/logproto.Pusher/Push\"}[5m])) by (route)",
                "legendFormat": "{{route}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Ingester Success Rate",
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
                "decimals": null,
                "format": "percentunit",
                "label": "",
                "logBase": 1,
                "max": "1",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 10,
                "w": 12,
                "x": 0,
                "y": 16
            },
            "id": 10,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "hideEmpty": true,
                "hideZero": false,
                "max": false,
                "min": false,
                "rightSide": true,
                "show": true,
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
                "expr": "histogram_quantile(0.99, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/querier\", route=~\"api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values\", cluster=\"$cluster\"}))",
                "legendFormat": "{{route}}-.99",
                "refId": "A"
                },
                {
                "expr": "histogram_quantile(0.9, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/querier\", route=~\"api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values\", cluster=\"$cluster\"}))",
                "legendFormat": "{{route}}-.9",
                "refId": "B"
                },
                {
                "expr": "histogram_quantile(0.5, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/querier\", route=~\"api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values\", cluster=\"$cluster\"}))",
                "legendFormat": "{{route}}-.5",
                "refId": "C"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Query Latency",
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
            "datasource": "$datasource",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 12,
                "y": 16
            },
            "id": 14,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "histogram_quantile(0.99, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/querier\", route=~\"api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values\", cluster=\"$cluster\"})) * 1e3",
                "legendFormat": ".99-{{route}}",
                "refId": "A"
                },
                {
                "expr": "histogram_quantile(0.9, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/querier\", route=~\"api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values\", cluster=\"$cluster\"})) * 1e3",
                "legendFormat": ".9-{{route}}",
                "refId": "B"
                },
                {
                "expr": "histogram_quantile(0.5, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/querier\", route=~\"api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values\", cluster=\"$cluster\"})) * 1e3",
                "legendFormat": ".5-{{route}}",
                "refId": "C"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Querier Latency",
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
            "datasource": "$datasource",
            "fill": 0,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 18,
                "y": 16
            },
            "id": 73,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "sum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/querier\", status_code=~\"success|200\"}[5m])) by (route)\n/\nsum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/querier\"}[5m])) by (route)",
                "legendFormat": "{{route}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Querier Success Rate",
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
                "decimals": null,
                "format": "percentunit",
                "label": "",
                "logBase": 1,
                "max": "1",
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
            "datasource": "$datasource",
            "description": "",
            "fill": 1,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 12,
                "y": 21
            },
            "id": 15,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "histogram_quantile(0.99, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/ingester\", route=~\"/logproto.Querier/Query|/logproto.Querier/Label\", cluster=\"$cluster\"})) * 1e3",
                "legendFormat": ".99-{{route}}",
                "refId": "A"
                },
                {
                "expr": "histogram_quantile(0.9, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/ingester\", route=~\"/logproto.Querier/Query|/logproto.Querier/Label\", cluster=\"$cluster\"})) * 1e3",
                "legendFormat": ".9-{{route}}",
                "refId": "B"
                },
                {
                "expr": "histogram_quantile(0.5, sum by (le,route) (job_route:loki_request_duration_seconds_bucket:sum_rate{job=\"$namespace/ingester\", route=~\"/logproto.Querier/Query|/logproto.Querier/Label\", cluster=\"$cluster\"})) * 1e3",
                "legendFormat": ".5-{{route}}",
                "refId": "C"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Ingester Latency",
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
            "datasource": "$datasource",
            "fill": 0,
            "fillGradient": 0,
            "gridPos": {
                "h": 5,
                "w": 6,
                "x": 18,
                "y": 21
            },
            "id": 74,
            "legend": {
                "alignAsTable": true,
                "avg": false,
                "current": false,
                "max": false,
                "min": false,
                "rightSide": true,
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
                "expr": "sum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\", status_code=~\"success|200\", route=~\"/logproto.Querier/Query|/logproto.Querier/Label\"}[5m])) by (route)\n/\nsum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\", route=~\"/logproto.Querier/Query|/logproto.Querier/Label\"}[5m])) by (route)",
                "legendFormat": "{{route}}",
                "refId": "A"
                }
            ],
            "thresholds": [],
            "timeFrom": null,
            "timeRegions": [],
            "timeShift": null,
            "title": "Ingester Success Rate",
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
                "decimals": null,
                "format": "percentunit",
                "label": "",
                "logBase": 1,
                "max": "1",
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
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 26
            },
            "id": 23,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 2
                },
                "id": 26,
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
                "pointradius": 1,
                "points": true,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "namespace_pod_name_container_name:container_cpu_usage_seconds_total:sum_rate{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"distributor.*\"}",
                    "intervalFactor": 3,
                    "legendFormat": "{{pod_name}}-{{container_name}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "CPU Usage",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 6,
                    "y": 2
                },
                "id": 27,
                "legend": {
                    "avg": false,
                    "current": false,
                    "hideEmpty": false,
                    "hideZero": false,
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
                "pointradius": 1,
                "points": true,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "container_memory_usage_bytes{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"distributor.*\"}",
                    "instant": false,
                    "intervalFactor": 3,
                    "legendFormat": "{{pod_name}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Memory Usage",
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
                    "h": 3,
                    "w": 12,
                    "x": 12,
                    "y": 2
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
                    "color": "#FF9830"
                    },
                    {
                    "alias": "error",
                    "color": "#F2495C"
                    },
                    {
                    "alias": "info",
                    "color": "#73BF69"
                    },
                    {
                    "alias": "debug",
                    "color": "#5794F2"
                    }
                ],
                "spaceLength": 10,
                "stack": true,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate({cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/distributor\", level!~\"debug|info\"}[5m])) by (level)",
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
                    "h": 18,
                    "w": 12,
                    "x": 12,
                    "y": 5
                },
                "id": 29,
                "options": {
                    "showTime": false,
                    "sortOrder": "Descending"
                },
                "targets": [
                    {
                    "expr": "{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/distributor\", level!~\"info|debug\"}",
                    "refId": "A"
                    }
                ],
                "timeFrom": null,
                "timeShift": null,
                "title": "Logs",
                "type": "logs"
                },
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 0,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 9
                },
                "id": 33,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/distributor\", status_code=~\"success|200\"}[5m])) by (route)\n/\nsum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/distributor\"}[5m])) by (route)",
                    "intervalFactor": 1,
                    "legendFormat": "{{route}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Success Rate",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 6,
                    "y": 9
                },
                "id": 32,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(loki_distributor_ingester_append_failures_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (ingester)",
                    "intervalFactor": 1,
                    "legendFormat": "{{ingester}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Append Failures By Ingester",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 16
                },
                "id": 34,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(loki_distributor_bytes_received_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (instance)",
                    "intervalFactor": 1,
                    "legendFormat": "{{instance}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Bytes Received/Second",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 6,
                    "y": 16
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(loki_distributor_lines_received_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (instance)",
                    "intervalFactor": 1,
                    "legendFormat": "{{instance}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Lines Received/Second",
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
                }
            ],
            "title": "Distributor",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 27
            },
            "id": 19,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 3
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
                "lines": false,
                "linewidth": 1,
                "nullPointMode": "null",
                "options": {
                    "dataLinks": []
                },
                "percentage": false,
                "pointradius": 1,
                "points": true,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "namespace_pod_name_container_name:container_cpu_usage_seconds_total:sum_rate{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"ingester.*\"}",
                    "intervalFactor": 3,
                    "legendFormat": "{{pod_name}}-{{container_name}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "CPU Usage",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 6,
                    "y": 3
                },
                "id": 37,
                "legend": {
                    "avg": false,
                    "current": false,
                    "hideEmpty": false,
                    "hideZero": false,
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
                "pointradius": 1,
                "points": true,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "container_memory_usage_bytes{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"ingester.*\"}",
                    "instant": false,
                    "intervalFactor": 3,
                    "legendFormat": "{{pod_name}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Memory Usage",
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
                    "h": 3,
                    "w": 12,
                    "x": 12,
                    "y": 3
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
                    "color": "#FF9830"
                    },
                    {
                    "alias": "error",
                    "color": "#F2495C"
                    },
                    {
                    "alias": "info",
                    "color": "#73BF69"
                    },
                    {
                    "alias": "debug",
                    "color": "#5794F2"
                    }
                ],
                "spaceLength": 10,
                "stack": true,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate({cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\", level!~\"debug|info\"}[5m])) by (level)",
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
                    "h": 18,
                    "w": 12,
                    "x": 12,
                    "y": 6
                },
                "id": 39,
                "options": {
                    "showTime": false,
                    "sortOrder": "Descending"
                },
                "targets": [
                    {
                    "expr": "{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\", level!~\"info|debug\"}",
                    "refId": "A"
                    }
                ],
                "timeFrom": null,
                "timeShift": null,
                "title": "Logs",
                "type": "logs"
                },
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 0,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 10
                },
                "id": 67,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\", status_code=~\"success|200\"}[5m])) by (route)\n/\nsum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/ingester\"}[5m])) by (route)",
                    "intervalFactor": 1,
                    "legendFormat": "{{route}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Success Rate",
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
                }
            ],
            "title": "Ingester",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 28
            },
            "id": 64,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 4
                },
                "id": 68,
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
                "pointradius": 1,
                "points": true,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "namespace_pod_name_container_name:container_cpu_usage_seconds_total:sum_rate{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"querier.*\"}",
                    "intervalFactor": 3,
                    "legendFormat": "{{pod_name}}-{{container_name}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "CPU Usage",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 6,
                    "y": 4
                },
                "id": 69,
                "legend": {
                    "avg": false,
                    "current": false,
                    "hideEmpty": false,
                    "hideZero": false,
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
                "pointradius": 1,
                "points": true,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "container_memory_usage_bytes{cluster=\"$cluster\", namespace=\"$namespace\", pod_name=~\"querier.*\"}",
                    "instant": false,
                    "intervalFactor": 3,
                    "legendFormat": "{{pod_name}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Memory Usage",
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
                    "h": 3,
                    "w": 12,
                    "x": 12,
                    "y": 4
                },
                "id": 65,
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
                    "color": "#FF9830"
                    },
                    {
                    "alias": "error",
                    "color": "#F2495C"
                    },
                    {
                    "alias": "info",
                    "color": "#73BF69"
                    },
                    {
                    "alias": "debug",
                    "color": "#5794F2"
                    }
                ],
                "spaceLength": 10,
                "stack": true,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate({cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/querier\", level!~\"debug|info\"}[5m])) by (level)",
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
                    "h": 18,
                    "w": 12,
                    "x": 12,
                    "y": 7
                },
                "id": 66,
                "options": {
                    "showTime": false,
                    "sortOrder": "Descending"
                },
                "targets": [
                    {
                    "expr": "{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/querier\", level!~\"info|debug\"}",
                    "refId": "A"
                    }
                ],
                "timeFrom": null,
                "timeShift": null,
                "title": "Logs",
                "type": "logs"
                },
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 0,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 11
                },
                "id": 70,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/querier\", status_code=~\"success|200\"}[5m])) by (route)\n/\nsum(rate(loki_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", job=\"$namespace/querier\"}[5m])) by (route)",
                    "intervalFactor": 1,
                    "legendFormat": "{{route}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Success Rate",
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
                }
            ],
            "title": "Querier",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 29
            },
            "id": 52,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 22
                },
                "id": 53,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_memcache_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (method, name, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".99-{{method}}-{{name}}",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_memcache_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (method, name, le))",
                    "hide": false,
                    "legendFormat": ".9-{{method}}-{{name}}",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_memcache_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (method, name, le))",
                    "hide": false,
                    "legendFormat": ".5-{{method}}-{{name}}",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Latency By Method",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 30
                },
                "id": 54,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_memcache_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (status_code, method, name)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}-{{method}}-{{name}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Status By Method",
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
                }
            ],
            "title": "Memcached",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 30
            },
            "id": 57,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 31
                },
                "id": 55,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_consul_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".99-{{operation}}",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_consul_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".9-{{operation}}",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_consul_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".5-{{operation}}",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Latency By Operation",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 39
                },
                "id": 58,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_consul_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, status_code, method)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}-{{operation}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Status By Operation",
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
                }
            ],
            "title": "Consul",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 31
            },
            "id": 43,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 5
                },
                "id": 41,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/MutateRows\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".9",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/MutateRows\"}[5m])) by (operation, le))",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/MutateRows\"}[5m])) by (operation, le))",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "MutateRows Latency",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 6,
                    "y": 5
                },
                "id": 46,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/ReadRows\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".9",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/ReadRows\"}[5m])) by (operation, le))",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/ReadRows\"}[5m])) by (operation, le))",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "ReadRows Latency",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 12,
                    "y": 5
                },
                "id": 44,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/GetTable\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".9",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/GetTable\"}[5m])) by (operation, le))",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/GetTable\"}[5m])) by (operation, le))",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "GetTable Latency",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 18,
                    "y": 5
                },
                "id": 45,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/ListTables\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".9",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/ListTables\"}[5m])) by (operation, le))",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_bigtable_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/ListTables\"}[5m])) by (operation, le))",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "ListTables Latency",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 0,
                    "y": 12
                },
                "id": 47,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_bigtable_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/MutateRows\"}[5m])) by (status_code)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "MutateRows Status",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 6,
                    "y": 12
                },
                "id": 50,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_bigtable_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.v2.Bigtable/ReadRows\"}[5m])) by (status_code)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "ReadRows Status",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 12,
                    "y": 12
                },
                "id": 48,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_bigtable_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/GetTable\"}[5m])) by (status_code)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "GetTable Status",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 7,
                    "w": 6,
                    "x": 18,
                    "y": 12
                },
                "id": 49,
                "interval": "",
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_bigtable_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\", operation=\"/google.bigtable.admin.v2.BigtableTableAdmin/ListTables\"}[5m])) by (status_code)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "ListTables Status",
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
                }
            ],
            "title": "Big Table",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 32
            },
            "id": 60,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 8
                },
                "id": 61,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_gcs_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".99-{{operation}}",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_gcs_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".9-{{operation}}",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_gcs_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".5-{{operation}}",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Latency By Operation",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 16
                },
                "id": 62,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_gcs_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (status_code, operation)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}-{{operation}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Status By Method",
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
                }
            ],
            "title": "GCS",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 33
            },
            "id": 76,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": null,
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 6,
                    "w": 6,
                    "x": 0,
                    "y": 9
                },
                "id": 82,
                "legend": {
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "show": true,
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
                    "expr": "sum(rate(cortex_dynamo_failures_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m]))",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Failure Rate",
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
                "datasource": null,
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 6,
                    "w": 6,
                    "x": 6,
                    "y": 9
                },
                "id": 83,
                "legend": {
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "show": true,
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
                    "expr": "sum(rate(cortex_dynamo_consumed_capacity_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m]))",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Consumed Capacity Rate",
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
                "datasource": null,
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 6,
                    "w": 6,
                    "x": 12,
                    "y": 9
                },
                "id": 84,
                "legend": {
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "show": true,
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
                    "expr": "sum(rate(cortex_dynamo_throttled_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m]))",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Throttled Rate",
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
                "datasource": null,
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 6,
                    "w": 6,
                    "x": 18,
                    "y": 9
                },
                "id": 85,
                "legend": {
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "show": true,
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
                    "expr": "sum(rate(cortex_dynamo_dropped_requests_total{cluster=\"$cluster\", namespace=\"$namespace\"}[5m]))",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Dropped Rate",
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
                "datasource": null,
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 6,
                    "w": 6,
                    "x": 0,
                    "y": 15
                },
                "id": 86,
                "legend": {
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "show": true,
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
                    "expr": "histogram_quantile(.99, sum(rate(cortex_dynamo_query_pages_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])))",
                    "legendFormat": ".99",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_dynamo_query_pages_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])))",
                    "legendFormat": ".9",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_dynamo_query_pages_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])))",
                    "legendFormat": ".5",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Query Pages",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 6,
                    "w": 9,
                    "x": 6,
                    "y": 15
                },
                "id": 87,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_dynamo_request_duration_seconds_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".99-{{operation}}",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_dynamo_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".9-{{operation}}",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_dynamo_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".5-{{operation}}",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Latency By Operation",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 6,
                    "w": 9,
                    "x": 15,
                    "y": 15
                },
                "id": 88,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_dynamo_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (status_code, operation)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}-{{operation}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Status By Method",
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
                }
            ],
            "title": "Dynamo",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 34
            },
            "id": 78,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 10
                },
                "id": 79,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_s3_request_duration_seconds_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".99-{{operation}}",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_s3_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".9-{{operation}}",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_s3_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".5-{{operation}}",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Latency By Operation",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 18
                },
                "id": 80,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_s3_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (status_code, operation)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}-{{operation}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Status By Method",
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
                }
            ],
            "title": "S3",
            "type": "row"
            },
            {
            "collapsed": true,
            "datasource": null,
            "gridPos": {
                "h": 1,
                "w": 24,
                "x": 0,
                "y": 35
            },
            "id": 90,
            "panels": [
                {
                "aliasColors": {},
                "bars": false,
                "dashLength": 10,
                "dashes": false,
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 36
                },
                "id": 91,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "histogram_quantile(.99, sum(rate(cortex_cassandra_request_duration_seconds_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "intervalFactor": 1,
                    "legendFormat": ".99-{{operation}}",
                    "refId": "A"
                    },
                    {
                    "expr": "histogram_quantile(.9, sum(rate(cortex_cassandra_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".9-{{operation}}",
                    "refId": "B"
                    },
                    {
                    "expr": "histogram_quantile(.5, sum(rate(cortex_cassandra_request_duration_seconds_bucket{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (operation, le))",
                    "hide": false,
                    "legendFormat": ".5-{{operation}}",
                    "refId": "C"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Latency By Operation",
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
                "datasource": "$datasource",
                "fill": 1,
                "fillGradient": 0,
                "gridPos": {
                    "h": 8,
                    "w": 24,
                    "x": 0,
                    "y": 44
                },
                "id": 92,
                "interval": "",
                "legend": {
                    "alignAsTable": true,
                    "avg": false,
                    "current": false,
                    "max": false,
                    "min": false,
                    "rightSide": true,
                    "show": true,
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
                "pointradius": 1,
                "points": false,
                "renderer": "flot",
                "seriesOverrides": [],
                "spaceLength": 10,
                "stack": false,
                "steppedLine": false,
                "targets": [
                    {
                    "expr": "sum(rate(cortex_cassandra_request_duration_seconds_count{cluster=\"$cluster\", namespace=\"$namespace\"}[5m])) by (status_code, operation)",
                    "intervalFactor": 1,
                    "legendFormat": "{{status_code}}-{{operation}}",
                    "refId": "A"
                    }
                ],
                "thresholds": [],
                "timeFrom": null,
                "timeRegions": [],
                "timeShift": null,
                "title": "Status By Method",
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
                }
            ],
            "title": "Cassandra",
            "type": "row"
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
                "text": "ops-tools1",
                "value": "ops-tools1"
                },
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "datasource",
                "options": [],
                "query": "prometheus",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": false,
                "type": "datasource"
            },
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
                "allValue": null,
                "current": {
                "tags": [],
                "text": "ops-tools1",
                "value": "ops-tools1"
                },
                "datasource": "$datasource",
                "definition": "label_values(kube_pod_container_info{image=~\".*loki.*\"}, cluster)",
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "cluster",
                "options": [
                {
                    "selected": false,
                    "text": "eks-autodesk-us-east-1",
                    "value": "eks-autodesk-us-east-1"
                },
                {
                    "selected": false,
                    "text": "eu-west2",
                    "value": "eu-west2"
                },
                {
                    "selected": false,
                    "text": "hg-free-us-central1",
                    "value": "hg-free-us-central1"
                },
                {
                    "selected": false,
                    "text": "hm-us-east2",
                    "value": "hm-us-east2"
                },
                {
                    "selected": false,
                    "text": "hm-us-west2",
                    "value": "hm-us-west2"
                },
                {
                    "selected": true,
                    "text": "ops-tools1",
                    "value": "ops-tools1"
                },
                {
                    "selected": false,
                    "text": "qa-us-central1",
                    "value": "qa-us-central1"
                },
                {
                    "selected": false,
                    "text": "us-central1",
                    "value": "us-central1"
                },
                {
                    "selected": false,
                    "text": "us-east2",
                    "value": "us-east2"
                },
                {
                    "selected": false,
                    "text": "us-west1",
                    "value": "us-west1"
                },
                {
                    "selected": false,
                    "text": "worldping-us-central2",
                    "value": "worldping-us-central2"
                }
                ],
                "query": "label_values(kube_pod_container_info{image=~\".*loki.*\"}, cluster)",
                "refresh": 0,
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
                "datasource": "$datasource",
                "definition": "label_values(kube_pod_container_info{image=~\".*loki.*\", cluster=\"$cluster\"}, namespace)",
                "hide": 0,
                "includeAll": false,
                "label": null,
                "multi": false,
                "name": "namespace",
                "options": [
                {
                    "selected": true,
                    "text": "tempo-dev",
                    "value": "tempo-dev"
                },
                {
                    "selected": false,
                    "text": "cortex-ops",
                    "value": "cortex-ops"
                },
                {
                    "selected": false,
                    "text": "default",
                    "value": "default"
                }
                ],
                "query": "label_values(kube_pod_container_info{image=~\".*loki.*\", cluster=\"$cluster\"}, namespace)",
                "refresh": 0,
                "regex": "",
                "skipUrlSync": false,
                "sort": 0,
                "tagValuesQuery": "",
                "tags": [],
                "tagsQuery": "",
                "type": "query",
                "useTags": false
            }
            ]
        },
        "time": {
            "from": "now-3h",
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
        "title": "Loki Operational",
        "uid": "f6fe30815b172c9da7e810c15ddfe607",
        "version": 1
        }
  },
}
