local vendor_config = import 'github.com/grafana/mimir/operations/mimir-mixin/config.libsonnet';
local vendor_utils = import 'github.com/grafana/mimir/operations/mimir-mixin/dashboards/dashboard-utils.libsonnet';
local g = import 'grafana-builder/grafana.libsonnet';
local grafana = import 'grafonnet/grafana.libsonnet';

{
  _config+:: {
    canary+: {
      enabled: false,
    },
  },
  grafanaDashboards+: if !$._config.canary.enabled then {} else {
    local dashboard = (
      vendor_utils {
        _config:: vendor_config._config + $._config {
          product: 'Loki',
          dashboard_prefix: 'Loki / ',
          tags: ['loki'],
        },
      }
    ),
    'loki-canary.json':
      // The dashboard() function automatically adds the "Loki / " prefix to the dashboard title.
      // This logic is inherited from mimir-mixin.
      dashboard.dashboard('Canary')
      // We can't make use of simplified template selectors from the loki dashboard utils until we port the cortex dashboard utils panel/grid functionality.
      .addTemplate('cluster', 'loki_build_info', $._config.per_cluster_label)
      .addTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', 'namespace')
      + {
        // This dashboard uses the new grid system in order to place panels (using gridPos).
        // Because of this we can't use the mixin's addRow() and addPanel().
        schemaVersion: 27,
        rows: null,
        // ugly hack, copy pasta the tag/link
        // code from the loki-mixin
        tags: $._config.tags,
        links: [
          {
            asDropdown: true,
            icon: 'external link',
            includeVars: true,
            keepTime: true,
            tags: $._config.tags,
            targetBlank: false,
            title: 'Loki Dashboards',
            type: 'dashboards',
          },
        ],
        panels: [
          // grid row 1
          dashboard.panel('Canary Entries Total') +
          dashboard.newStatPanel('sum(count(loki_canary_entries_total{' + $._config.per_cluster_label + '=~"$cluster", namespace=~"$namespace"}))', unit='short') +
          { gridPos: { h: 4, w: 3, x: 0, y: 0 } },

          dashboard.panel('Canary Logs Total') +
          dashboard.newStatPanel('sum(increase(loki_canary_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))', unit='short') +
          { gridPos: { h: 4, w: 3, x: 3, y: 0 } },

          dashboard.panel('Missing') +
          dashboard.newStatPanel('sum(increase(loki_canary_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))', unit='short') +
          { gridPos: { h: 4, w: 3, x: 6, y: 0 } },

          dashboard.panel('Spotcheck Missing') +
          dashboard.newStatPanel('sum(increase(loki_canary_spot_check_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))', unit='short') +
          { gridPos: { h: 4, w: 3, x: 9, y: 0 } },

          // grid row 2
          dashboard.panel('Spotcheck Total') +
          dashboard.newStatPanel('sum(increase(loki_canary_spot_check_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))', unit='short') +
          { gridPos: { h: 4, w: 3, x: 0, y: 4 } },

          dashboard.panel('Metric Test Error %') +
          dashboard.newStatPanel('((sum(loki_canary_metric_test_expected{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}) - sum(loki_canary_metric_test_actual{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}))/(sum(loki_canary_metric_test_actual{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}))) * 100') +
          { gridPos: { h: 4, w: 3, x: 3, y: 4 } },

          dashboard.panel('Missing %') +
          dashboard.newStatPanel('(sum(increase(loki_canary_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))/sum(increase(loki_canary_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range])))*100') +
          { gridPos: { h: 4, w: 3, x: 6, y: 4 } },

          dashboard.panel('Spotcheck Missing %') +
          dashboard.newStatPanel('(sum(increase(loki_canary_spot_check_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))/sum(increase(loki_canary_spot_check_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))) * 100') +
          { gridPos: { h: 4, w: 3, x: 9, y: 4 } },

          // grid row 3
          dashboard.panel('Metric Test Expected') +
          dashboard.newStatPanel('sum(loki_canary_metric_test_expected{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"})', unit='short') +
          { gridPos: { h: 4, w: 3, x: 0, y: 8 } },

          dashboard.panel('Metric Test Actual') +
          dashboard.newStatPanel('sum(loki_canary_metric_test_actual{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"})', unit='short') +
          { gridPos: { h: 4, w: 3, x: 3, y: 8 } },

          dashboard.panel('Websocket Missing') +
          dashboard.newStatPanel('sum(increase(loki_canary_websocket_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))', unit='short') +
          { gridPos: { h: 4, w: 3, x: 6, y: 8 } },

          dashboard.panel('Websocket Missing %') +
          dashboard.newStatPanel('(sum(increase(loki_canary_websocket_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range]))/sum(increase(loki_canary_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__range])))*100') +
          { gridPos: { h: 4, w: 3, x: 9, y: 8 } },
          // end of grid

          dashboard.panel('Log Write to read Latency Percentiles') +
          dashboard.queryPanel([
            'histogram_quantile(0.95, sum(rate(loki_canary_response_latency_seconds_bucket{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval])) by (le))',
            'histogram_quantile(0.50, sum(rate(loki_canary_response_latency_seconds_bucket{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval])) by (le))',
          ], ['p95', 'p50']) +
          { gridPos: { h: 6, w: 12, x: 12, y: 0 } },

          grafana.heatmapPanel.new(
            'Log Write to Read Latency',
            datasource='$datasource',
            tooltip_showHistogram=true,
            color_colorScheme='interpolateReds',
            legend_show=false,
          ).addTargets(
            [
              grafana.prometheus.target(
                'sum(rate(loki_canary_response_latency_seconds_bucket{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval])) by (le)',
                legendFormat='{{le}}',
                format='heatmap',
              ),
            ],
          ) +
          { gridPos: { h: 6, w: 12, x: 12, y: 12 } },

          dashboard.panel('Spot Check Query') +
          dashboard.queryPanel([
            'histogram_quantile(0.99, sum(rate(loki_canary_spot_check_request_duration_seconds_bucket{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval])) by (le))',
            'histogram_quantile(0.50, sum(rate(loki_canary_spot_check_request_duration_seconds_bucket{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval])) by (le))',
          ], ['p99', 'p95']) +
          { gridPos: { h: 6, w: 12, x: 0, y: 14 } },

          dashboard.panel('Metric Test Query') +
          dashboard.queryPanel([
            'histogram_quantile(0.99, sum(rate(loki_canary_metric_test_request_duration_seconds_bucket{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[15m])) by (le))',
            'histogram_quantile(0.50, sum(rate(loki_canary_metric_test_request_duration_seconds_bucket{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[15m])) by (le))',
          ], ['p99', 'p95'],) +
          { gridPos: { h: 6, w: 12, x: 12, y: 14 } },

          dashboard.panel('Spot Check Missing %') +
          dashboard.queryPanel('topk(20, (sum by (' + $._config.per_cluster_label + ', pod) (increase(loki_canary_spot_check_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval]))/sum by (' + $._config.per_cluster_label + ', pod) (increase(loki_canary_spot_check_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval])) * 100)) > 0', '') +
          { gridPos: { h: 6, w: 12, x: 0, y: 20 } },

          g.panel('Missing logs') +
          g.queryPanel('topk(20,(sum by (' + $._config.per_cluster_label + ', pod)(increase(loki_canary_missing_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval]))/sum by (' + $._config.per_cluster_label + ', pod)(increase(loki_canary_entries_total{' + $._config.per_cluster_label + '=~"$cluster",namespace=~"$namespace"}[$__rate_interval])))*100) > 0', 'Missing {{ ' + $._config.per_cluster_label + ' }} {{ pod }}') +
          { gridPos: { h: 6, w: 12, x: 12, y: 20 } },

        ],
      },
  },
}
