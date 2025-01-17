local configfile = import 'config.libsonnet';
local g = import 'grafana-builder/grafana.libsonnet';
local loki_mixin_utils = import 'loki-mixin/dashboards/dashboard-utils.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+:: {
    local dashboard = (
      loki_mixin_utils {
        _config+:: configfile._config,
      }
    ),
    local dashboards = self,

    local labelsSelector = dashboard._config.per_cluster_label + '=~"$cluster", namespace=~"$namespace"',
    local quantileLabelSelector = dashboard._config.per_cluster_label + '=~"$cluster", job=~"$namespace/promtail.*"',

    'promtail.json': {
                       local cfg = self,
                     } +
                     dashboard.dashboard('Loki / Promtail', uid='promtail')
                     .addTemplate('cluster', 'promtail_build_info', dashboard._config.per_cluster_label)
                     .addTag()
                     .addTemplate('namespace', 'promtail_build_info{' + dashboard._config.per_cluster_label + '=~"$cluster"}', 'namespace')
                     .addRow(
                       g.row('Targets & Files')
                       .addPanel(
                         g.panel('Active Targets') +
                         g.queryPanel(
                           'sum(promtail_targets_active_total{%s})' % labelsSelector,
                           'Active Targets',
                         ),
                       )
                       .addPanel(
                         g.panel('Active Files') +
                         g.queryPanel(
                           'sum(promtail_files_active_total{%s})' % labelsSelector,
                           'Active Targets',
                         ),
                       )
                     )
                     .addRow(
                       g.row('IO')
                       .addPanel(
                         g.panel('Bps') +
                         g.queryPanel(
                           'sum(rate(promtail_read_bytes_total{%s}[$__rate_interval]))' % labelsSelector,
                           'logs read',
                         ) +
                         { yaxes: g.yaxes('Bps') },
                       )
                       .addPanel(
                         g.panel('Lines') +
                         g.queryPanel(
                           'sum(rate(promtail_read_lines_total{%s}[$__rate_interval]))' % labelsSelector,
                           'lines read',
                         ),
                       )
                     )
                     .addRow(
                       g.row('Requests')
                       .addPanel(
                         g.panel('QPS') +
                         g.qpsPanel('promtail_request_duration_seconds_count{%s}' % labelsSelector)
                       )
                       .addPanel(
                         g.panel('Latency') +
                         g.queryPanel(
                           [
                             'job:promtail_request_duration_seconds:99quantile{%s}' % quantileLabelSelector,
                             'job:promtail_request_duration_seconds:50quantile{%s}' % quantileLabelSelector,
                             'job:promtail_request_duration_seconds:avg{%s}' % quantileLabelSelector,
                           ],
                           ['p99', 'p50', 'avg']
                         )
                       )
                     ),
  },
}
