(import 'dashboard-utils.libsonnet') {
  local ingester_pod_matcher = if $._config.meta_monitoring.enabled
  then 'container=~"loki|ingester|partition-ingester", pod=~"(ingester.*|partition-ingester.*|loki-single-binary)"'
  else 'container=~"ingester|partition-ingester"',
  local ingester_job_matcher = if $._config.meta_monitoring.enabled
  then '(ingester.*|partition-ingester.*|loki-single-binary)'
  else '(ingester.*|partition-ingester.*)',

  grafanaDashboards+:: if $._config.ssd.enabled then {} else {
    'loki-writes-resources.json':
      ($.dashboard('Loki / Writes Resources', uid='writes-resources'))
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
      .addRow(
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
        $.row('Ingester')
        .addPanel(
          $.newQueryPanel('In-memory streams') +
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
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            'sum by(%s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $.filterNodeDisk(ingester_pod_matcher)],
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            'sum by(%s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $.filterNodeDisk(ingester_pod_matcher)],
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', ingester_job_matcher),
        )
      ),
  },
}
