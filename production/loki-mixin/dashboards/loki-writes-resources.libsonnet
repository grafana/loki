local grafana = import 'grafonnet/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local ingester_pod_matcher = if !$._config.ssd then 'container="ingester"' else 'container="loki", pod=~"(enterprise-logs|loki)-write.*"',
  local ingester_job_matcher = if !$._config.ssd then 'ingester' else '(enterprise-logs|loki)-write',

  grafanaDashboards+::
    {
      'loki-writes-resources.json':
        ($.dashboard('Loki / Writes Resources', uid='writes-resources'))
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
          $.row('Distributor')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'distributor'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'distributor'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'distributor'),
          )
        )
        .addRow(
          grafana.row.new(if !$._config.ssd then 'Ingester' else 'Write path')
          .addPanel(
            $.panel('In-memory streams') +
            $.queryPanel(
              'sum by(%s) (loki_ingester_memory_streams{%s})' % [$._config.per_instance_label, $.jobMatcher(ingester_job_matcher)],
              '{{%s}}' % $._config.per_instance_label
            ) +
            {
              tooltip: { sort: 2 },  // Sort descending.
            },
          )
          .addPanel(
            $.CPUUsagePanel('CPU', ingester_pod_matcher),
          )
          .addPanel(
            $.memoryWorkingSetPanel('Memory (workingset)', ingester_pod_matcher),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', ingester_job_matcher),
          )
          .addPanel(
            $.panel('Disk Writes') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(ingester_pod_matcher)],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.panel('Disk Reads') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(ingester_pod_matcher)],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', ingester_job_matcher),
          )
        ),
    },
}
