(import 'dashboard-utils.libsonnet') {
  local index_gateway_pod_matcher = if $._config.meta_monitoring.enabled
  then 'container=~"loki|index-gateway", ' + $._config.per_instance_label + '=~"(*.index-gateway.*|loki-single-binary)"'
  else 'container="index-gateway"',
  local index_gateway_job_matcher = if $._config.meta_monitoring.enabled
  then '(.*index-gateway.*|loki-single-binary)'
  else 'index-gateway',

  local ingester_pod_matcher = if $._config.meta_monitoring.enabled
  then 'container=~"loki|ingester|partition-ingester", ' + $._config.per_instance_label + '=~"(.*ingester.*|loki-single-binary)"'
  else 'container=~"ingester|partition-ingester"',
  local ingester_job_matcher = if $._config.meta_monitoring.enabled
  then '(.*ingester.*|loki-single-binary)'
  else '(.*ingester|partition-ingester).*',

  grafanaDashboards+:: {
    'loki-reads-resources.json':
      ($.dashboard('Loki / Reads Resources', uid='reads-resources'))
      .addCluster()
      .addNamespace()
      .addTag()

      // --- Gateway ---
      .addRowIf(
        $._config.internal_components,
        $.componentRow(
          'Gateway',
          $.containerCPUUsagePanel('CPU', 'cortex-gw(-internal)?'),
          $.containerMemoryWorkingSetPanel('Memory (working set)', 'cortex-gw(-internal)?'),
          $.goHeapAllocPanel('Memory (Go heap allocated)', 'cortex-gw(-internal)?'),
          'container=~"cortex-gw(-internal)?"',
          'container=~"cortex-gw(-internal)?"',
          'gateway'
        )
      )

      // --- Query Frontend ---
      .addRow($.componentRow(
        'Query-frontend',
        $.containerCPUUsagePanel('CPU', 'query-frontend'),
        $.containerMemoryWorkingSetPanel('Memory (working set)', 'query-frontend'),
        $.goHeapAllocPanel('Memory (Go heap allocated)', 'query-frontend'),
        'container="query-frontend"',
        'container="query-frontend"',
        'query_frontend'
      ))

      // --- Query Scheduler ---
      .addRow($.componentRow(
        'Query-scheduler',
        $.containerCPUUsagePanel('CPU', 'query-scheduler'),
        $.containerMemoryWorkingSetPanel('Memory (working set)', 'query-scheduler'),
        $.goHeapAllocPanel('Memory (Go heap allocated)', 'query-scheduler'),
        'container="query-scheduler"',
        'container="query-scheduler"',
        'query_scheduler'
      ))

      // --- Querier ---
      .addRow($.componentRow(
        'Querier',
        $.containerCPUUsagePanel('CPU', 'querier'),
        $.containerMemoryWorkingSetPanel('Memory (working set)', 'querier'),
        $.goHeapAllocPanel('Memory (Go heap allocated)', 'querier'),
        'container="querier"',
        'container="querier"',
        'querier',
        trailingPanels=[
          $.diskWritesPanel('container="querier"'),
          $.diskReadsPanel('container="querier"'),
          $.containerDiskSpaceUtilizationPanel('Disk space utilization', 'querier'),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Index Gateway ---
      .addRow($.componentRow(
        'Index gateway',
        $.CPUUsagePanel('CPU', index_gateway_pod_matcher),
        $.memoryWorkingSetPanel('Memory (working set)', index_gateway_pod_matcher),
        $.goHeapAllocPanel('Memory (Go heap allocated)', index_gateway_job_matcher),
        index_gateway_pod_matcher,
        index_gateway_pod_matcher,
        'index_gateway',
        trailingPanels=[
          $.diskWritesPanel(index_gateway_pod_matcher),
          $.diskReadsPanel(index_gateway_pod_matcher),
          $.containerDiskSpaceUtilizationPanel('Disk space utilization', index_gateway_job_matcher),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Bloom Gateway ---
      .addRow($.componentRow(
        'Bloom gateway',
        $.containerCPUUsagePanel('CPU', 'bloom-gateway'),
        $.containerMemoryWorkingSetPanel('Memory (working set)', 'bloom-gateway'),
        $.goHeapAllocPanel('Memory (Go heap allocated)', 'bloom-gateway'),
        'container="bloom-gateway"',
        'container="bloom-gateway"',
        'bloom_gateway',
        trailingPanels=[
          $.diskWritesPanel('container="bloom-gateway"'),
          $.diskReadsPanel('container="bloom-gateway"'),
          $.containerDiskSpaceUtilizationPanel('Disk space utilization', 'bloom-gateway'),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Ingester ---
      .addRow($.componentRow(
        'Ingester',
        $.CPUUsagePanel('CPU', ingester_pod_matcher),
        $.memoryWorkingSetPanel('Memory (working set)', ingester_pod_matcher),
        $.goHeapAllocPanel('Memory (Go heap allocated)', ingester_job_matcher),
        ingester_pod_matcher,
        ingester_pod_matcher,
        'ingester'
      ))

      // --- Ruler ---
      .addRow($.componentRow(
        'Ruler',
        $.containerCPUUsagePanel('CPU', 'ruler'),
        $.containerMemoryWorkingSetPanel('Memory (working set)', 'ruler'),
        $.goHeapAllocPanel('Memory (Go heap allocated)', 'ruler'),
        'container="ruler"',
        'container="ruler"',
        'ruler',
        trailingPanels=[
          $.newQueryPanel('Rules') +
          $.queryPanel(
            'sum by(%(label)s) (loki_prometheus_rule_group_rules{%(matcher)s}) or sum by(%(label)s) (cortex_prometheus_rule_group_rules{%(matcher)s})' % { label: $._config.per_instance_label, matcher: $.jobMatcher('ruler') },
            '{{%s}}' % $._config.per_instance_label
          ),
        ],
        trailingSpans=[6],
      )),
  },
}
