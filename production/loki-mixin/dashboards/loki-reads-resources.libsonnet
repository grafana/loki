local grafana = import 'grafonnet/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local index_gateway_pod_matcher = if $._config.ssd then 'container="loki", pod=~"(enterprise-logs|loki)-read.*"' else 'container="index-gateway"',
  local index_gateway_job_matcher = if $._config.ssd then '(enterprise-logs|loki)-read' else 'index-gateway',

  local ingester_pod_matcher = if $._config.ssd then 'container="loki", pod=~"(enterprise-logs|loki)-write.*"' else 'container="ingester"',
  local ingester_job_matcher = if $._config.ssd then '(enterprise-logs|loki)-write' else 'ingester',

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
            $.containerCPUUsagePanel('CPU', 'cortex-gw'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'cortex-gw'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'cortex-gw'),
          )
        )
        .addRowIf(
          !$._config.ssd,
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
          !$._config.ssd,
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
          !$._config.ssd,
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
            $.panel('Disk Writes') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('querier')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.panel('Disk Reads') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('querier')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'querier'),
          )
        )
        .addRow(
          grafana.row.new(if $._config.ssd then 'Read path' else 'Index Gateway')
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
            $.panel('Disk Writes') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(index_gateway_pod_matcher)],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.panel('Disk Reads') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(index_gateway_pod_matcher)],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', index_gateway_job_matcher),
          )
          .addPanel(
            $.panel('Query Readiness Duration') +
            $.queryPanel(
              ['loki_boltdb_shipper_query_readiness_duration_seconds{%s}' % $.namespaceMatcher()], ['duration']
            ) +
            { yaxes: $.yaxes('s') },
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
          !$._config.ssd,
          grafana.row.new('Ruler')
          .addPanel(
            $.panel('Rules') +
            $.queryPanel(
              'sum by(%s) (cortex_prometheus_rule_group_rules{%s})' % [$._config.per_instance_label, $.jobMatcher('ruler')],
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
