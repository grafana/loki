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
          self.addMultiTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', 'namespace')
        else
          self.addTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', 'namespace'),

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
          .addMultiTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', 'namespace')
        else
          d.addTemplate('cluster', 'loki_build_info', $._config.per_cluster_label)
          .addTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', 'namespace'),
    },

  jobMatcher(job)::
    $._config.per_cluster_label + '=~"$cluster", job=~"($namespace)/%s"' % job,

  namespaceMatcher()::
    $._config.per_cluster_label + '=~"$cluster", namespace=~"$namespace"',

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
          expr: '%s{%s} * 1e3' % [metric_name, $.namespaceMatcher()],
          refId: 'A',
          instant: true,
          format: 'time_series',
        },
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
}
