local utils = import 'mixin-utils/utils.libsonnet';

(import 'loki-mixin/dashboards/dashboard-utils.libsonnet') {
  grafanaDashboards+:: {

    local dashboards = self,

    'promtail.json': {
                       local cfg = self,
                       labelsSelector:: $._config.dashboard_labels_selector,
                       quantileLabelSelector:: $._config.dashboard_quantile_label_selector,
                     } +
                     $.dashboard($._config.dashboard_name, uid='promtail')
                     .addTag()
                     .addTemplate('cluster', 'promtail_build_info', $._config.per_cluster_label)
                     .addTemplate('namespace', 'promtail_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', 'namespace')
                     .addRow(
                       $.row('Targets & Files')
                       .addPanel(
                         $.panel('Active Targets') +
                         $.queryPanel(
                           'sum(promtail_targets_active_total{%s})' % dashboards['promtail.json'].labelsSelector,
                           'Active Targets',
                         ),
                       )
                       .addPanel(
                         $.panel('Active Files') +
                         $.queryPanel(
                           'sum(promtail_files_active_total{%s})' % dashboards['promtail.json'].labelsSelector,
                           'Active Targets',
                         ),
                       )
                     )
                     .addRow(
                       $.row('IO')
                       .addPanel(
                         $.panel('Bps') +
                         $.queryPanel(
                           'sum(rate(promtail_read_bytes_total{%s}[$__rate_interval]))' % dashboards['promtail.json'].labelsSelector,
                           'logs read',
                         ) +
                         { yaxes: $.yaxes('Bps') },
                       )
                       .addPanel(
                         $.panel('Lines') +
                         $.queryPanel(
                           'sum(rate(promtail_read_lines_total{%s}[$__rate_interval]))' % dashboards['promtail.json'].labelsSelector,
                           'lines read',
                         ),
                       )
                     )
                     .addRow(
                       $.row('Requests')
                       .addPanel(
                         $.panel('QPS') +
                         $.qpsPanel('promtail_request_duration_seconds_count{%s}' % dashboards['promtail.json'].labelsSelector)
                       )
                       .addPanel(
                         $.panel('Latency') +
                         $.queryPanel(
                           [
                             'job:promtail_request_duration_seconds:99quantile{%s}' % dashboards['promtail.json'].quantileLabelSelector,
                             'job:promtail_request_duration_seconds:50quantile{%s}' % dashboards['promtail.json'].quantileLabelSelector,
                             'job:promtail_request_duration_seconds:avg{%s}' % dashboards['promtail.json'].quantileLabelSelector,
                           ],
                           ['p99', 'p50', 'avg']
                         )
                       )
                     ),
  },
}
