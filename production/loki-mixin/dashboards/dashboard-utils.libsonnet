local vendor_config = import 'github.com/grafana/mimir/operations/mimir-mixin/config.libsonnet';
local vendor_utils = import 'github.com/grafana/mimir/operations/mimir-mixin/dashboards/dashboard-utils.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'grafana-builder/grafana.libsonnet') {
  // Override the dashboard constructor to add:
  // - default tags,
  // - some links that propagate the selected cluster.
  dashboard(title, uid='')::
    super.dashboard(title, uid) + {
      addRowIf(condition, row)::
        if condition
        then self.addRow(row)
        else self,
      addLog(name='loki_datasource'):: self {
        templating+: {
          list+: [
            {
              hide: 0,
              label: null,
              name: name,
              options: [],
              query: 'loki',
              refresh: 1,
              regex: '',
              type: 'datasource',
            },
          ],
        },
      },

      addCluster(multi=false)::
        if multi then
          self.addMultiTemplate('cluster', 'loki_build_info', $._config.per_cluster_label)
        else
          self.addTemplate('cluster', 'loki_build_info', $._config.per_cluster_label),

      addNamespace(multi=false)::
        if multi then
          self.addMultiTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', $._config.per_namespace_label)
        else
          self.addTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', $._config.per_namespace_label),

      addTag()::
        self + {
          tags+: $._config.tags,
          links+: [
            {
              asDropdown: true,
              icon: 'external link',
              includeVars: true,
              keepTime: true,
              tags: $._config.tags,
              targetBlank: false,
              title: 'Loki Dashboards',
              type: 'dashboards',
            },
          ],
        },

      addClusterSelectorTemplates(multi=true)::
        local d = self {
          tags: $._config.tags,
          links: [
            {
              asDropdown: true,
              icon: 'external link',
              includeVars: true,
              keepTime: true,
              tags: $._config.tags,
              targetBlank: false,
              title: 'Loki Dashboards',
              type: 'dashboards',
            },
          ],
        };

        if multi then
          d.addMultiTemplate('cluster', 'loki_build_info', $._config.per_cluster_label)
          .addMultiTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', $._config.per_namespace_label)
        else
          d.addTemplate('cluster', 'loki_build_info', $._config.per_cluster_label)
          .addTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', $._config.per_namespace_label),
    },

  jobMatcher(job)::
    $._config.per_cluster_label + '=~"$cluster", job=~"($namespace)/%s"' % job,

  namespaceMatcher()::
    $._config.per_cluster_label + '=~"$cluster", ' + $._config.per_namespace_label + '=~"$namespace"',

  logPanel(title, selector, datasource='$loki_datasource'):: {
    title: title,
    type: 'logs',
    datasource: datasource,
    targets: [
      {
        refId: 'A',
        expr: selector,
      },
    ],
  },
  fromNowPanel(title, metric_name)::
    local metrics =
      if std.isString(metric_name) then [metric_name]
      else if std.isArray(metric_name) then metric_name
      else error 'invalid type for metric_name';
    $.panel(title) +
    {
      type: 'stat',
      title: title,
      fieldConfig: {
        defaults: {
          custom: {},
          thresholds: {
            mode: 'absolute',
            steps: [
              {
                color: 'green',
                value: null,
              },
            ],
          },
          color: {
            mode: 'fixed',
            fixedColor: 'blue',
          },
          unit: 'dateTimeFromNow',
        },
      },
      targets: [
        {
          expr: '%s{%s} * 1e3' % [m, $.namespaceMatcher()],
          instant: true,
          format: 'time_series',
        }
        for m in metrics
      ],
      options: {
        reduceOptions: {
          values: false,
          calcs: [
            'lastNotNull',
          ],
          fields: '',
        },
        orientation: 'auto',
        text: {},
        textMode: 'auto',
        colorMode: 'value',
        graphMode: 'area',
        justifyMode: 'auto',
      },
      datasource: '$datasource',
    },
  CPUUsagePanel(title, matcher)::
    $.newQueryPanel(title) +
    $.queryPanel([
      'sum by(pod) (rate(container_cpu_usage_seconds_total{%s, %s}[$__rate_interval]))' % [$.namespaceMatcher(), matcher],
      'min(kube_pod_container_resource_requests{%s, %s, resource="cpu"} > 0)' % [$.namespaceMatcher(), matcher],
      'min(container_spec_cpu_quota{%s, %s} / container_spec_cpu_period{%s, %s})' % [$.namespaceMatcher(), matcher, $.namespaceMatcher(), matcher],
    ], ['{{pod}}', 'request', 'limit']) +
    {
      tooltip: { sort: 2 },  // Sort descending.
    } + {
      fieldConfig+: {
        overrides+: [
          $.colorOverride('request', '#FFC000') + {
            properties+: [
              {
                id: 'custom.fillOpacity',
                value: 0,
              },
            ],
          },
          $.colorOverride('limit', '#E02F44') + {
            properties+: [
              {
                id: 'custom.fillOpacity',
                value: 0,
              },
            ],
          },
        ],
      },
    },
  containerCPUUsagePanel(title, containerName)::
    self.CPUUsagePanel(title, 'container=~"%s"' % containerName),

  memoryWorkingSetPanel(title, matcher)::
    $.newQueryPanel(title, 'bytes') +
    $.queryPanel([
      // We use "max" instead of "sum" otherwise during a rolling update of a statefulset we will end up
      // summing the memory of the old pod (whose metric will be stale for 5m) to the new pod.
      'max by(pod) (container_memory_working_set_bytes{%s, %s})' % [$.namespaceMatcher(), matcher],
      'min(kube_pod_container_resource_requests{%s, %s, resource="memory"} > 0)' % [$.namespaceMatcher(), matcher],
      'min(container_spec_memory_limit_bytes{%s, %s} > 0)' % [$.namespaceMatcher(), matcher],
    ], ['{{pod}}', 'request', 'limit']) +
    {
      tooltip: { sort: 2 },  // Sort descending.
    } + {
      fieldConfig+: {
        overrides+: [
          $.colorOverride('request', '#FFC000') + {
            properties+: [
              {
                id: 'custom.fillOpacity',
                value: 0,
              },
            ],
          },
          $.colorOverride('limit', '#E02F44') + {
            properties+: [
              {
                id: 'custom.fillOpacity',
                value: 0,
              },
            ],
          },
        ],
      },
    },
  containerMemoryWorkingSetPanel(title, containerName)::
    self.memoryWorkingSetPanel(title, 'container=~"%s"' % containerName),

  goHeapInUsePanel(title, jobName)::
    $.newQueryPanel(title, 'bytes') +
    $.queryPanel(
      'sum by(%s) (go_memstats_heap_inuse_bytes{%s})' % [$._config.per_instance_label, $.jobMatcher(jobName)],
      '{{%s}}' % $._config.per_instance_label
    ) +
    {
      tooltip: { sort: 2 },  // Sort descending.
    },

  // Live heap objects, i.e. memory actually in use by the application.
  // Prefer this over goHeapInUsePanel: heap_inuse_bytes counts whole spans the
  // allocator has reserved, so it also includes allocator overhead and
  // fragmentation within partially used spans. heap_alloc_bytes is "memory we
  // actually use"; heap_inuse_bytes is "memory the Go runtime holds for current
  // and future allocations".
  goHeapAllocPanel(title, jobName)::
    $.newQueryPanel(title, 'bytes') +
    $.queryPanel(
      'sum by(%s) (go_memstats_heap_alloc_bytes{%s})' % [$._config.per_instance_label, $.jobMatcher(jobName)],
      '{{%s}}' % $._config.per_instance_label
    ) +
    {
      tooltip: { sort: 2 },  // Sort descending.
    },

  filterNodeDisk(matcher)::
    |||
      ignoring(%s) group_right() (label_replace(count by(%s, %s, device) (container_fs_writes_bytes_total{%s, %s, device!~".*sda.*"}), "device", "$1", "device", "/dev/(.*)") * 0)
    ||| % [$._config.per_instance_label, $._config.per_node_label, $._config.per_instance_label, $.namespaceMatcher(), matcher],
  filterNodeDiskContainer(containerName)::
    self.filterNodeDisk('container="%s"' % containerName),

  newQueryPanel(title, unit='short')::
    super.timeseriesPanel(title) + {
      fieldConfig+: {
        defaults+: {
          custom+: {
            fillOpacity: 10,
          },
          unit: unit,
        },
      },
    },

  withStacking:: {
    fieldConfig+: {
      defaults+: {
        custom+: {
          fillOpacity: 100,
          lineWidth: 0,
          stacking: {
            mode: 'normal',
            group: 'A',
          },
        },
      },
    },
  },

  colorOverride(name, color):: {
    matcher: {
      id: 'byName',
      options: name,
    },
    properties: [
      {
        id: 'color',
        value: {
          mode: 'fixed',
          fixedColor: color,
        },
      },
    ],
  },

  newQpsPanel(selector, statusLabelName='status_code')::
    super.qpsPanel(selector, statusLabelName) + $.withStacking + {
      fieldConfig+: {
        defaults+: {
          min: 0,
        },
        overrides: [
          $.colorOverride('1xx', '#EAB839'),
          $.colorOverride('2xx', '#7EB26D'),
          $.colorOverride('3xx', '#6ED0E0'),
          $.colorOverride('4xx', '#EF843C'),
          $.colorOverride('5xx', '#E24D42'),
          $.colorOverride('OK', '#7EB26D'),
          $.colorOverride('cancel', '#A9A9A9'),
          $.colorOverride('error', '#E24D42'),
          $.colorOverride('success', '#7EB26D'),
        ],
      },
    },

  newStatPanel(queries, legends='', unit='percentunit', decimals=1, thresholds=[], instant=false, novalue='')::
    super.queryPanel(queries, legends) + {
      type: 'stat',
      targets: [
        target {
          instant: instant,
          interval: '',

          // Reset defaults from queryPanel().
          format: null,
          intervalFactor: null,
          step: null,
        }
        for target in super.targets
      ],
      fieldConfig: {
        defaults: {
          decimals: decimals,
          thresholds: {
            mode: 'absolute',
            steps: thresholds,
          },
          noValue: novalue,
          unit: unit,
        },
        overrides: [],
      },
    },

  containerDiskSpaceUtilizationPanel(title, containerName)::
    $.newQueryPanel(title, 'percentunit') +
    $.queryPanel('max by(persistentvolumeclaim) (kubelet_volume_stats_used_bytes{%s, persistentvolumeclaim=~".*%s.*"} / kubelet_volume_stats_capacity_bytes{%s, persistentvolumeclaim=~".*%s.*"})' % [$.namespaceMatcher(), containerName, $.namespaceMatcher(), containerName], '{{persistentvolumeclaim}}'),

  local latencyPanelWithExtraGrouping(metricName, selector, multiplier='1e3', extra_grouping='') = {
    nullPointMode: 'null as zero',
    targets: [
      {
        expr: 'histogram_quantile(0.99, sum(rate(%s_bucket%s[$__rate_interval])) by (le,%s)) * %s' % [metricName, selector, extra_grouping, multiplier],
        format: 'time_series',
        intervalFactor: 2,
        refId: 'A',
        step: 10,
        interval: '1m',
        legendFormat: '__auto',
      },
    ],
  },

  p99LatencyByPod(metric, selectorStr)::
    $.newQueryPanel('Per Pod Latency (p99)', 'ms') +
    latencyPanelWithExtraGrouping(metric, selectorStr, '1e3', 'pod'),

  //
  // Shared building blocks for the "Resources" dashboards.
  //

  // Regex matching the "type" label on the loki_autoscaler_* metrics, per
  // component. Follows the naming convention emitted by the loki-autoscaler
  // exporter.
  autoscalerType:: {
    gateway: 'cortex_gateway(_internal)?',
    distributor: 'distributor',
    query_frontend: 'query-frontend',
    query_scheduler: 'query-scheduler',
    querier: 'querier',
    index_gateway: 'index-gateway',
    bloom_gateway: 'bloom-gateway',
    ingester: '(partition-)?ingester',
    ruler: 'ruler',
  },

  // withNoValue replaces the "No data" a panel renders when its query
  // returns nothing with a custom message.
  withNoValue(text):: {
    fieldConfig+: {
      defaults+: {
        noValue: text,
      },
    },
  },

  oomKilledPanel(matcher)::
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
    ) +
    $.withNoValue('No OOMs observed in time period 🎉'),

  // Grouped by job rather than summed, so that zone-replicated components
  // report one series per zone. The partition ingesters, for example, run each
  // zone as its own StatefulSet but export a single per-zone min/max replicas
  // series, so a summed pod count compares N zones of pods against one zone's
  // limit. Grouping keeps both panels in the same units, and surfaces any
  // imbalance between zones.
  // The job label carries a "<namespace>/" prefix that is redundant here (the
  // namespace is already a dashboard variable), so strip it for the legend.
  // Series whose job has no prefix are left untouched by label_replace.
  runningPodsPanel(matcher)::
    $.newQueryPanel('Running Pods') +
    $.queryPanel(
      'label_replace(count by (%(job)s) (up{%(ns)s, %(m)s}), "%(job)s", "$1", "%(job)s", ".*/(.*)")' % {
        job: $._config.per_job_label,
        ns: $.namespaceMatcher(),
        m: matcher,
      },
      '{{%s}}' % $._config.per_job_label
    ),

  minReplicasPanel(component)::
    $.panel('Min Replicas') +
    $.newStatPanel(
      'sum(loki_autoscaler_min_replicas{%s, type=~"%s"})' % [$.namespaceMatcher(), $.autoscalerType[component]],
      unit='short',
      decimals=0,
      novalue='Not auto-scaled',
    ),

  maxReplicasPanel(component)::
    $.panel('Max Replicas') +
    $.newStatPanel(
      'sum(loki_autoscaler_max_replicas{%s, type=~"%s"})' % [$.namespaceMatcher(), $.autoscalerType[component]],
      unit='short',
      decimals=0,
      novalue='Not auto-scaled',
    ),

  // autoscalingPanels returns the leading Min/Max Replicas panels + spans for
  // a component's row, or nothing at all when autoscaling_metrics is disabled.
  //
  // The panels are rendered whether or not the component is actually
  // auto-scaled: when the autoscaler exports no metrics for it they display
  // "Not auto-scaled" instead of a number, so there is no build-time list of
  // auto-scaled components to keep in sync.
  autoscalingPanels(component)::
    if !$._config.autoscaling_metrics then
      { panels: [], spans: [] }
    else
      {
        panels: [$.minReplicasPanel(component), $.maxReplicasPanel(component)],
        spans: [2, 2],
      },

  //
  // HPA/KEDA autoscaling rows for the Reads and Writes dashboards.
  //

  // Mimir's dashboard helpers configured with the Loki config, so the HPA
  // replicas panel is shared with Mimir rather than copied.
  //
  // The KEDA panels are Loki's own: Loki's ScaledObject triggers are unnamed,
  // so KEDA reports all of them as scaler="prometheusScaler" and Mimir's
  // per-scaler (cpu/memory) panels would show nothing useful. They group by
  // ScaledObject and trigger metric (s0-prometheus, s1-cron-..., ...) instead.
  local mimirUtils = vendor_utils {
    _config:: vendor_config._config + $._config {
      alert_aggregation_labels: $._config.group_by_cluster,
    },
  },

  // autoscalingDesiredReplicasPanel shows the replicas each trigger asks for,
  // before the HPA min/max bounds are applied. AverageValue triggers want
  // value / target replicas; Value triggers (e.g. the partition ingesters)
  // want value / target * current replicas.
  autoscalingDesiredReplicasPanel(component)::
    local params = {
      ns: $.namespaceMatcher(),
      cluster_label: $._config.per_cluster_label,
      group_by: $._config.group_by_cluster,
      hpa_name: $._config.autoscaling[component].hpa_name,
      hpa_prefix: $._config.autoscaling_hpa_prefix,
    };
    local desiredReplicas(targetType, multiplier) = |||
      sum by (scaledObject, metric) (
        # Using `max by ()` so that series churn doesn't break the promQL join
        max by (%(group_by)s, scaledObject, metric) (
          label_replace(
            keda_scaler_metrics_value{%(cluster_label)s=~"$cluster", exported_namespace=~"$namespace"},
            "namespace", "$1", "exported_namespace", "(.*)"
          )
        )
        / on (%(group_by)s, scaledObject, metric) group_left
        max by (%(group_by)s, scaledObject, metric) (
          label_replace(
            label_replace(
              kube_horizontalpodautoscaler_spec_target_metric{%(ns)s, horizontalpodautoscaler=~"%(hpa_name)s", metric_target_type="%(target_type)s"},
              "metric", "$1", "metric_name", "(.+)"
            ),
            "scaledObject", "$1", "horizontalpodautoscaler", "%(hpa_prefix)s(.*)"
          )
        )%(multiplier)s
      )
    ||| % (params { target_type: targetType, multiplier: multiplier });
    local timesCurrentReplicas = |||
      * on (%(group_by)s, scaledObject) group_left
      max by (%(group_by)s, scaledObject) (
        label_replace(
          kube_horizontalpodautoscaler_status_current_replicas{%(ns)s, horizontalpodautoscaler=~"%(hpa_name)s"},
          "scaledObject", "$1", "horizontalpodautoscaler", "%(hpa_prefix)s(.*)"
        )
      )
    ||| % params;

    $.newQueryPanel('Desired replicas') +
    $.queryPanel(
      '(\n%s)\nor\n(\n%s)' % [
        desiredReplicas('average', ''),
        desiredReplicas('value', std.strReplace('\n' + std.rstripChars(timesCurrentReplicas, '\n'), '\n', '\n  ')),
      ],
      '{{scaledObject}} {{metric}}',
    ) +
    mimirUtils.panelDescription(
      'Desired replicas',
      |||
        The number of replicas each autoscaling trigger asks for: the scaling metric exposed by KEDA
        divided by its target. It ignores the min/max replicas constraints applied later by the HPA.
      |||
    ),

  // autoscalingFailuresPanel shows the rate at which KEDA fails to read the
  // scaling metrics, during which the autoscaler can't work properly. KEDA
  // 2.16 renamed keda_scaler_errors to keda_scaler_detail_errors_total, so
  // match both.
  autoscalingFailuresPanel(component)::
    $.newQueryPanel('Autoscaler failures rate') +
    $.queryPanel(
      |||
        sum by (scaledObject, metric) (
          # Using `max by ()` so duplicate series for the same trigger (both metric
          # names, or several KEDA operator replicas) aren't counted twice.
          max by (%(group_by)s, scaledObject, metric) (
            label_replace(
              rate({__name__=~"keda_scaler_detail_errors_total|keda_scaler_errors", %(cluster_label)s=~"$cluster", exported_namespace=~"$namespace"}[$__rate_interval]),
              "namespace", "$1", "exported_namespace", "(.+)"
            )
          )
          + on (%(group_by)s, scaledObject, metric) group_left
          # Using `max by ()` so that series churn doesn't break the promQL join
          max by (%(group_by)s, scaledObject, metric) (
            label_replace(
              label_replace(
                kube_horizontalpodautoscaler_spec_target_metric{%(ns)s, horizontalpodautoscaler=~"%(hpa_name)s"} * 0,
                "metric", "$1", "metric_name", "(.+)"
              ),
              "scaledObject", "$1", "horizontalpodautoscaler", "%(hpa_prefix)s(.*)"
            )
          )
        )
      ||| % {
        ns: $.namespaceMatcher(),
        cluster_label: $._config.per_cluster_label,
        group_by: $._config.group_by_cluster,
        hpa_name: $._config.autoscaling[component].hpa_name,
        hpa_prefix: $._config.autoscaling_hpa_prefix,
      },
      '{{scaledObject}} {{metric}} failures',
    ) +
    mimirUtils.panelDescription(
      'Autoscaler failures rate',
      |||
        The rate of failures in the KEDA custom metrics API server. Whenever an error occurs, the KEDA custom
        metrics server is unable to query the scaling metric from Prometheus so the autoscaler wouldn't work properly.
      |||
    ),

  // autoscalingRow builds the "<title> - autoscaling" row for one of the
  // components defined in $._config.autoscaling.
  autoscalingRow(title, component)::
    $.row('%s - autoscaling' % title)
    .addPanel(mimirUtils.autoScalingActualReplicas(component))
    .addPanel($.autoscalingDesiredReplicasPanel(component))
    .addPanel($.autoscalingFailuresPanel(component)),

  diskWritesPanel(matcher)::
    $.newQueryPanel('Disk Writes', 'Bps') +
    $.queryPanel(
      'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(matcher)],
      '{{%s}} - {{device}}' % $._config.per_instance_label
    ) +
    $.withStacking,

  diskReadsPanel(matcher)::
    $.newQueryPanel('Disk Reads', 'Bps') +
    $.queryPanel(
      'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(matcher)],
      '{{%s}} - {{device}}' % $._config.per_instance_label
    ) +
    $.withStacking,

  // componentRow builds a per-component row. Panels are packed onto 12-unit
  // visual lines and wrap onto subsequent lines when the running total
  // exceeds 12, letting a single (collapsible) row hold several visual lines.
  // Layouts:
  //   autoscaling_metrics off (OSS default):
  //     Line 1: [RunningPods=4, OOMs=4, CPU=4]
  //     Line 2: [Memory ws=6, Memory heap=6]
  //   autoscaling_metrics on:
  //     Line 1: [MinReplicas=2, MaxReplicas=2, RunningPods=4, CPU=4]
  //     Line 2: [OOMs=4, Memory ws=4, Memory heap=4]
  // trailingPanels/trailingSpans are appended after the core panels (e.g.
  // Disk Writes/Reads/Space, Rules), and are expected to add up to 12.
  componentRow(
    title,
    cpuPanel,
    memoryPanel,
    goHeapPanel,
    runningPodsMatcher,
    oomMatcher,
    autoscalingComponent,
    trailingPanels=[],
    trailingSpans=[],
  )::
    local as = $.autoscalingPanels(autoscalingComponent);
    local hasAS = std.length(as.panels) > 0;
    local oomPanel = $.oomKilledPanel(oomMatcher);
    local podsPanel = $.runningPodsPanel(runningPodsMatcher);
    local corePanels =
      if hasAS then
        // Autoscaling-on layout: replicas/status on line 1 with RunningPods
        // and CPU; OOMs + memory on line 2.
        as.panels + [podsPanel, cpuPanel, oomPanel, memoryPanel, goHeapPanel]
      else
        // Autoscaling-off layout: RunningPods/OOMs/CPU on line 1; memory on
        // line 2.
        [podsPanel, oomPanel, cpuPanel, memoryPanel, goHeapPanel];
    local coreSpans = if hasAS then as.spans + [4, 4, 4, 4, 4] else [4, 4, 4, 6, 6];
    local spans = coreSpans + trailingSpans;
    local row = std.foldl(
      function(r, p) r.addPanel(p),
      corePanels + trailingPanels,
      $.row(title),
    );
    // Override the spans auto-assigned by addPanel() with our explicit list.
    row {
      panels: [
        row.panels[i] { span: spans[i] }
        for i in std.range(0, std.length(spans) - 1)
      ],
    },
}
