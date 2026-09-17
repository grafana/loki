(import 'dashboard-utils.libsonnet') {
  local ingester_pod_matcher = if $._config.meta_monitoring.enabled
  then 'container=~"loki|ingester|partition-ingester", ' + $._config.per_instance_label + '=~"(.*ingester.*|loki-single-binary)"'
  else 'container=~"ingester|partition-ingester"',
  local ingester_job_matcher = if $._config.meta_monitoring.enabled
  then '(.*ingester.*|loki-single-binary)'
  else '(.*ingester.*)',

  grafanaDashboards+:: {
    'loki-writes-resources.json':
      ($.dashboard('Loki / Writes Resources', uid='writes-resources'))
      .addCluster()
      .addNamespace()
      .addTag()

      // --- Gateway ---
      .addRowIf(
        $._config.internal_components,
        $.componentRow(
          'Gateway',
          $.containerCPUUsagePanel('CPU', 'cortex-gw(-internal)?'),
          $.containerMemoryWorkingSetPanel('Memory (workingset)', 'cortex-gw(-internal)?'),
          $.goHeapAllocPanel('Memory (go heap alloc)', 'cortex-gw(-internal)?'),
          'container=~"cortex-gw(-internal)?"',
          'container=~"cortex-gw(-internal)?"',
          'gateway'
        )
      )

      // --- Distributor ---
      .addRow($.componentRow(
        'Distributor',
        $.containerCPUUsagePanel('CPU', 'distributor'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'distributor'),
        $.goHeapAllocPanel('Memory (go heap alloc)', 'distributor'),
        'container="distributor"',
        'container="distributor"',
        'distributor'
      ))

      // --- Ingester ---
      .addRow($.componentRow(
        'Ingester',
        $.CPUUsagePanel('CPU', ingester_pod_matcher),
        $.memoryWorkingSetPanel('Memory (workingset)', ingester_pod_matcher),
        $.goHeapAllocPanel('Memory (go heap alloc)', ingester_job_matcher),
        ingester_pod_matcher,
        ingester_pod_matcher,
        'ingester',
        trailingPanels=[
          $.newQueryPanel('In-memory streams') +
          $.queryPanel(
            'sum by(%s) (loki_ingester_memory_streams{%s})' % [$._config.per_instance_label, $.jobMatcher(ingester_job_matcher)],
            '{{%s}}' % $._config.per_instance_label
          ) +
          {
            tooltip: { sort: 2 },  // Sort descending.
          },
          $.diskWritesPanel(ingester_pod_matcher),
          $.diskReadsPanel(ingester_pod_matcher),
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', ingester_job_matcher),
        ],
        trailingSpans=[3, 3, 3, 3],
      )),
  },
}
