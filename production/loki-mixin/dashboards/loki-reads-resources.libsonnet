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

  // Regex for the "type" label on the autoscaler metric, per component.
  // Follows the naming convention emitted by the loki-autoscaler exporter.
  local autoscaler_type = {
    gateway: 'cortex_gateway(_internal)?',
    query_frontend: 'query-frontend',
    query_scheduler: 'query-scheduler',
    querier: 'querier',
    index_gateway: 'index-gateway',
    bloom_gateway: 'bloom-gateway',
    ingester: '(partition-)?ingester',
    ruler: 'ruler',
  },

  local oomKilledPanel(matcher) =
    $.newQueryPanel('OOMs') +
    $.queryPanel(
      |||
        sum(
          increase(kube_pod_container_status_restarts_total{%(ns)s, %(m)s}[$__rate_interval]) > 0
          and on(pod)
          kube_pod_container_status_last_terminated_reason{%(m)s, reason="OOMKilled"} == 1
        )
      ||| % { ns: $.namespaceMatcher(), m: matcher },
      'OOMs'
    ),

  local runningPodsPanel(matcher) =
    $.newQueryPanel('Running Pods') +
    $.queryPanel(
      'count(up{%s, %s})' % [$.namespaceMatcher(), matcher],
      'pods'
    ),

  local minReplicasPanel(component) =
    $.panel('Min Replicas') +
    $.newStatPanel(
      'sum(loki_autoscaler_min_replicas{%s, type=~"%s"})' % [$.namespaceMatcher(), autoscaler_type[component]],
      unit='short',
      decimals=0,
    ),

  local maxReplicasPanel(component) =
    $.panel('Max Replicas') +
    $.newStatPanel(
      'sum(loki_autoscaler_max_replicas{%s, type=~"%s"})' % [$.namespaceMatcher(), autoscaler_type[component]],
      unit='short',
      decimals=0,
    ),

  local notAutoScaledPanel = {
    type: 'text',
    title: 'Autoscaling',
    options: {
      mode: 'markdown',
      content: '### Not auto-scaled',
    },
  },

  // Returns the leading panels + spans for a component's row:
  // - Min/Max Replicas stats when autoscaling_metrics is enabled and the
  //   component is flagged auto-scaled.
  // - A "Not auto-scaled" text panel when autoscaling_metrics is enabled
  //   but the component is flagged not auto-scaled.
  // - Nothing when autoscaling_metrics is disabled.
  local autoscalingPanels(component) =
    if !$._config.autoscaling_metrics then { panels: [], spans: [] }
    else if $._config.autoscaled[component] then {
      panels: [minReplicasPanel(component), maxReplicasPanel(component)],
      spans: [2, 2],
    }
    else { panels: [notAutoScaledPanel], spans: [4] },

  local diskWritesPanel(matcher) =
    $.newQueryPanel('Disk Writes', 'Bps') +
    $.queryPanel(
      'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(matcher)],
      '{{%s}} - {{device}}' % $._config.per_instance_label
    ) +
    $.withStacking,

  local diskReadsPanel(matcher) =
    $.newQueryPanel('Disk Reads', 'Bps') +
    $.queryPanel(
      'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(matcher)],
      '{{%s}} - {{device}}' % $._config.per_instance_label
    ) +
    $.withStacking,

  // Override auto-distributed spans with an explicit list. Total span for
  // each row is 12; sums > 12 wrap to a new visual line, letting a single
  // (collapsible) row hold multiple visual lines of panels.
  local withSpans(spans) = {
    panels: [
      super.panels[i] { span: spans[i] }
      for i in std.range(0, std.length(spans) - 1)
    ],
  },

  // addPanels chains .addPanel(p) for each panel in the list, returning
  // the resulting row.
  local addPanels(row, panels) =
    std.foldl(function(r, p) r.addPanel(p), panels, row),

  // componentRow builds a per-component row. Panels are packed onto
  // 12-unit visual lines and wrap onto subsequent lines when the running
  // total exceeds 12. Layouts:
  //   autoscaling_metrics off (OSS default):
  //     Line 1: [RunningPods=4, OOMs=4, CPU=4]
  //     Line 2: [Memory ws=6, Memory heap=6]
  //   autoscaling on, component auto-scaled:
  //     Line 1: [MinReplicas=2, MaxReplicas=2, RunningPods=4, CPU=4]
  //     Line 2: [OOMs=4, Memory ws=4, Memory heap=4]
  //   autoscaling on, component not auto-scaled:
  //     Line 1: [Not-auto-scaled=4, RunningPods=4, CPU=4]
  //     Line 2: [OOMs=4, Memory ws=4, Memory heap=4]
  // trailingPanels/trailingSpans are appended after the core panels
  // (e.g. Disk Writes/Reads/Space, Rules).
  local componentRow(title, cpuPanel, memoryPanel, goHeapPanel, runningPodsMatcher, oomMatcher, autoscalingComponent, trailingPanels=[], trailingSpans=[]) =
    local as = autoscalingPanels(autoscalingComponent);
    local hasAS = std.length(as.panels) > 0;
    local corePanels =
      if hasAS then
        // Autoscaling-on layout: replicas/status on line 1 with RunningPods+CPU;
        // OOMs + memory on line 2.
        as.panels + [
          runningPodsPanel(runningPodsMatcher),
          cpuPanel,
          oomKilledPanel(oomMatcher),
          memoryPanel,
          goHeapPanel,
        ]
      else
        // Autoscaling-off layout: RunningPods/OOMs/CPU on line 1; memory on line 2.
        [
          runningPodsPanel(runningPodsMatcher),
          oomKilledPanel(oomMatcher),
          cpuPanel,
          memoryPanel,
          goHeapPanel,
        ];
    local coreSpans =
      if hasAS then
        as.spans + [4, 4, 4, 4, 4]
      else
        [4, 4, 4, 6, 6];
    addPanels($.row(title), corePanels + trailingPanels) + withSpans(coreSpans + trailingSpans),

  grafanaDashboards+:: {
    'loki-reads-resources.json':
      ($.dashboard('Loki / Reads Resources', uid='reads-resources'))
      .addCluster()
      .addNamespace()
      .addTag()

      // --- Gateway ---
      .addRowIf(
        $._config.internal_components,
        componentRow(
          'Gateway',
          $.containerCPUUsagePanel('CPU', 'cortex-gw(-internal)?'),
          $.containerMemoryWorkingSetPanel('Memory (workingset)', 'cortex-gw(-internal)?'),
          $.goHeapInUsePanel('Memory (go heap inuse)', 'cortex-gw(-internal)?'),
          'container=~"cortex-gw(-internal)?"',
          'container=~"cortex-gw(-internal)?"',
          'gateway'
        )
      )

      // --- Query Frontend ---
      .addRow(componentRow(
        'Query Frontend',
        $.containerCPUUsagePanel('CPU', 'query-frontend'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'query-frontend'),
        $.goHeapInUsePanel('Memory (go heap inuse)', 'query-frontend'),
        'container="query-frontend"',
        'container="query-frontend"',
        'query_frontend'
      ))

      // --- Query Scheduler ---
      .addRow(componentRow(
        'Query Scheduler',
        $.containerCPUUsagePanel('CPU', 'query-scheduler'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'query-scheduler'),
        $.goHeapInUsePanel('Memory (go heap inuse)', 'query-scheduler'),
        'container="query-scheduler"',
        'container="query-scheduler"',
        'query_scheduler'
      ))

      // --- Querier ---
      .addRow(componentRow(
        'Querier',
        $.containerCPUUsagePanel('CPU', 'querier'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'querier'),
        $.goHeapInUsePanel('Memory (go heap inuse)', 'querier'),
        'container="querier"',
        'container="querier"',
        'querier',
        trailingPanels=[
          diskWritesPanel('container="querier"'),
          diskReadsPanel('container="querier"'),
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'querier'),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Index Gateway ---
      .addRow(componentRow(
        'Index Gateway',
        $.CPUUsagePanel('CPU', index_gateway_pod_matcher),
        $.memoryWorkingSetPanel('Memory (workingset)', index_gateway_pod_matcher),
        $.goHeapInUsePanel('Memory (go heap inuse)', index_gateway_job_matcher),
        index_gateway_pod_matcher,
        index_gateway_pod_matcher,
        'index_gateway',
        trailingPanels=[
          diskWritesPanel(index_gateway_pod_matcher),
          diskReadsPanel(index_gateway_pod_matcher),
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', index_gateway_job_matcher),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Bloom Gateway ---
      .addRow(componentRow(
        'Bloom Gateway',
        $.containerCPUUsagePanel('CPU', 'bloom-gateway'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'bloom-gateway'),
        $.goHeapInUsePanel('Memory (go heap inuse)', 'bloom-gateway'),
        'container="bloom-gateway"',
        'container="bloom-gateway"',
        'bloom_gateway',
        trailingPanels=[
          diskWritesPanel('container="bloom-gateway"'),
          diskReadsPanel('container="bloom-gateway"'),
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'bloom-gateway'),
        ],
        trailingSpans=[4, 4, 4],
      ))

      // --- Ingester ---
      .addRow(componentRow(
        'Ingester',
        $.CPUUsagePanel('CPU', ingester_pod_matcher),
        $.memoryWorkingSetPanel('Memory (workingset)', ingester_pod_matcher),
        $.goHeapInUsePanel('Memory (go heap inuse)', ingester_job_matcher),
        ingester_pod_matcher,
        ingester_pod_matcher,
        'ingester'
      ))

      // --- Ruler ---
      .addRow(componentRow(
        'Ruler',
        $.containerCPUUsagePanel('CPU', 'ruler'),
        $.containerMemoryWorkingSetPanel('Memory (workingset)', 'ruler'),
        $.goHeapInUsePanel('Memory (go heap inuse)', 'ruler'),
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
