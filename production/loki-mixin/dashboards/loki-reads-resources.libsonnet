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
          $.containerMemoryWorkingSetPanel('Memory (workingset)', 'cortex-gw(-internal)?'),
          $.goHeapAllocPanel('Memory (go heap alloc)', 'cortex-gw(-internal)?'),
          'container=~"cortex-gw(-internal)?"',
          'container=~"cortex-gw(-internal)?"',
          'gateway'
        )
      )

      // --- Query Frontend ---
      .addRow($.componentRow(
        'Query Frontend',
        $.containerCPUUsagePanel('CPU', 'query-frontend'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'query-frontend'),
        $.goHeapAllocPanel('Memory (go heap alloc)', 'query-frontend'),
        'container="query-frontend"',
        'container="query-frontend"',
        'query_frontend'
      ))

      // --- Query Scheduler ---
      .addRow($.componentRow(
        'Query Scheduler',
        $.containerCPUUsagePanel('CPU', 'query-scheduler'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'query-scheduler'),
        $.goHeapAllocPanel('Memory (go heap alloc)', 'query-scheduler'),
        'container="query-scheduler"',
        'container="query-scheduler"',
        'query_scheduler'
      ))

      // --- Querier ---
      .addRow($.componentRow(
        'Querier',
        $.containerCPUUsagePanel('CPU', 'querier'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'querier'),
        $.goHeapAllocPanel('Memory (go heap alloc)', 'querier'),
        'container="querier"',
        'container="querier"',
        'querier',
        trailingPanels=[
          $.diskWritesPanel('container="querier"'),
          $.diskReadsPanel('container="querier"'),
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'querier'),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Index Gateway ---
      .addRow($.componentRow(
        'Index Gateway',
        $.CPUUsagePanel('CPU', index_gateway_pod_matcher),
        $.memoryWorkingSetPanel('Memory (workingset)', index_gateway_pod_matcher),
        $.goHeapAllocPanel('Memory (go heap alloc)', index_gateway_job_matcher),
        index_gateway_pod_matcher,
        index_gateway_pod_matcher,
        'index_gateway',
        trailingPanels=[
          $.diskWritesPanel(index_gateway_pod_matcher),
          $.diskReadsPanel(index_gateway_pod_matcher),
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', index_gateway_job_matcher),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Bloom Gateway ---
      .addRow($.componentRow(
        'Bloom Gateway',
        $.containerCPUUsagePanel('CPU', 'bloom-gateway'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'bloom-gateway'),
        $.goHeapAllocPanel('Memory (go heap alloc)', 'bloom-gateway'),
        'container="bloom-gateway"',
        'container="bloom-gateway"',
        'bloom_gateway',
        trailingPanels=[
          $.diskWritesPanel('container="bloom-gateway"'),
          $.diskReadsPanel('container="bloom-gateway"'),
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'bloom-gateway'),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Ingester ---
      .addRow($.componentRow(
        'Ingester',
        $.CPUUsagePanel('CPU', ingester_pod_matcher),
        $.memoryWorkingSetPanel('Memory (workingset)', ingester_pod_matcher),
        $.goHeapAllocPanel('Memory (go heap alloc)', ingester_job_matcher),
        ingester_pod_matcher,
        ingester_pod_matcher,
        'ingester'
      ))

      // --- Ruler ---
      .addRow($.componentRow(
        'Ruler',
        $.containerCPUUsagePanel('CPU', 'ruler'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'ruler'),
        $.goHeapAllocPanel('Memory (go heap alloc)', 'ruler'),
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
