local grafana = import 'grafonnet/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local index_gateway_pod_matcher = if $._config.meta_monitoring.enabled
  then 'container=~"loki|index-gateway", pod=~"(index-gateway.*|%s-read.*|loki-single-binary)"' % $._config.ssd.pod_prefix_matcher
  else if $._config.ssd.enabled then 'container="loki", pod=~"%s-read.*"' % $._config.ssd.pod_prefix_matcher else 'container="index-gateway"',
  local index_gateway_job_matcher = if $._config.meta_monitoring.enabled
  then '(index-gateway.*|%s-read.*|loki-single-binary)' % $._config.ssd.pod_prefix_matcher
  else if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'index-gateway',

  local ingester_pod_matcher = if $._config.meta_monitoring.enabled
  then 'container=~"loki|ingester", pod=~"(ingester.*|%s-write.*|loki-single-binary)"' % $._config.ssd.pod_prefix_matcher
  else if $._config.ssd.enabled then 'container="loki", pod=~"%s-write.*"' % $._config.ssd.pod_prefix_matcher else 'container="ingester"',
  local ingester_job_matcher = if $._config.meta_monitoring.enabled
  then '(ingester.+|%s-write|loki-single-binary)' % $._config.ssd.pod_prefix_matcher
  else if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester.+',

  grafanaDashboards+::
    {
      'loki-reads-resources.json':
        ($.dashboard('Loki / Reads Resources', uid='reads-resources'))
        .addCluster()
        .addNamespace()
        .addTag()
        .addRowIf(
          $._config.internal_components,
          $.row('Gateway')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'cortex-gw(-internal)?'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'cortex-gw(-internal)?'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'cortex-gw(-internal)?'),
          )
        )
        .addRowIf(
          !$._config.ssd.enabled,
          $.row('Query Frontend')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'query-frontend'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'query-frontend'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'query-frontend'),
          )
        )
        .addRowIf(
          !$._config.ssd.enabled,
          $.row('Query Scheduler')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'query-scheduler'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'query-scheduler'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'query-scheduler'),
          )
        )
        .addRowIf(
          !$._config.ssd.enabled,
          grafana.row.new('Querier')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'querier'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'querier'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'querier'),
          )
          .addPanel(
            $.newQueryPanel('Disk Writes', 'Bps') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('querier')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Disk Reads', 'Bps') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('querier')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.withStacking,
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'querier'),
          )
        )
        // Add the read path for single scalable deployment only. The read path should not display disk utilization as the index gateway is present in the backend pods.
        .addRowIf(
          $._config.ssd.enabled,
          grafana.row.new('Read path')
          .addPanel(
            $.CPUUsagePanel('CPU', index_gateway_pod_matcher),
          )
          .addPanel(
            $.memoryWorkingSetPanel('Memory (workingset)', index_gateway_pod_matcher),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', index_gateway_job_matcher),
          )
        )
        // Otherwise we add the index gateway information
        .addRowIf(
          !$._config.ssd.enabled,
          grafana.row.new('Index Gateway')
          .addPanel(
            $.CPUUsagePanel('CPU', index_gateway_pod_matcher),
          )
          .addPanel(
            $.memoryWorkingSetPanel('Memory (workingset)', index_gateway_pod_matcher),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', index_gateway_job_matcher),
          )
          .addPanel(
            $.newQueryPanel('Disk Writes', 'Bps') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(index_gateway_pod_matcher)],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Disk Reads', 'Bps') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(index_gateway_pod_matcher)],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.withStacking,
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', index_gateway_job_matcher),
          )
        )
        .addRowIf(
          !$._config.ssd.enabled,
          grafana.row.new('Bloom Gateway')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'bloom-gateway'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'bloom-gateway'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'bloom-gateway'),
          )
          .addPanel(
            $.newQueryPanel('Disk Writes', 'Bps') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('bloom-gateway')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Disk Reads', 'Bps') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('bloom-gateway')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.withStacking,
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'bloom-gateway'),
          )
        )
        .addRow(
          $.row('Ingester')
          .addPanel(
            $.CPUUsagePanel('CPU', ingester_pod_matcher),
          )
          .addPanel(
            $.memoryWorkingSetPanel('Memory (workingset)', ingester_pod_matcher),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', ingester_job_matcher),
          )
        )
        .addRowIf(
          !$._config.ssd.enabled,
          grafana.row.new('Ruler')
          .addPanel(
            $.newQueryPanel('Rules') +
            $.queryPanel(
              'sum by(%(label)s) (loki_prometheus_rule_group_rules{%(matcher)s}) or sum by(%(label)s) (cortex_prometheus_rule_group_rules{%(matcher)s})' % { label: $._config.per_instance_label, matcher: $.jobMatcher('ruler') },
              '{{%s}}' % $._config.per_instance_label
            ),
          )
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'ruler'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'ruler'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'ruler'),
          )
        ),
    },
}
