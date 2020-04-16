local g = import 'grafana-builder/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+: {
    'promtail.json':
      g.dashboard('Loki / Promtail')
      .addTemplate('cluster', 'kube_pod_container_info{image=~".*promtail.*"}', 'cluster')
      .addTemplate('namespace', 'kube_pod_container_info{image=~".*promtail.*"}', 'namespace')
      .addTemplate('name', 'kube_pod_info{namespace="$namespace",pod=~"promtail.*"}', 'created_by_name')
      .addRow(
        g.row('Targets & Files')
        .addPanel(
          g.panel('Active Targets') +
          g.queryPanel(
            'sum(promtail_targets_active_total{cluster="$cluster", job="$namespace/$name"})',
            'Active Targets',
          ),
        )
        .addPanel(
          g.panel('Active Files') +
          g.queryPanel(
            'sum(promtail_files_active_total{cluster="$cluster", job="$namespace/$name"})',
            'Active Targets',
          ),
        )
      )
      .addRow(
        g.row('IO')
        .addPanel(
          g.panel('Bps') +
          g.queryPanel(
            'sum(rate(promtail_read_bytes_total{cluster="$cluster", job="$namespace/$name"}[1m]))',
            'logs read',
          ) +
          { yaxes: g.yaxes('Bps') },
        )
        .addPanel(
          g.panel('Lines') +
          g.queryPanel(
            'sum(rate(promtail_read_lines_total{cluster="$cluster", job="$namespace/$name"}[1m]))',
            'lines read',
          ),
        )
      )
      .addRow(
        g.row('Requests')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('promtail_request_duration_seconds_count{cluster="$cluster", job="$namespace/$name"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('promtail_request_duration_seconds', [utils.selector.eq('job', '$namespace/$name')], extra_selectors=[utils.selector.eq('cluster', '$cluster')])
        )
      ),
  },
}
