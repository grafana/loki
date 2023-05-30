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

  containerLabelMatcher(containerName)::
    'label_name=~"%s.*"' % containerName,

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
    $.panel(title) +
    $.queryPanel([
      'sum by(pod) (rate(container_cpu_usage_seconds_total{%s, %s}[$__rate_interval]))' % [$.namespaceMatcher(), matcher],
      'min(kube_pod_container_resource_requests{%s, %s, resource="cpu"} > 0)' % [$.namespaceMatcher(), matcher],
      'min(container_spec_cpu_quota{%s, %s} / container_spec_cpu_period{%s, %s})' % [$.namespaceMatcher(), matcher, $.namespaceMatcher(), matcher],
    ], ['{{pod}}', 'request', 'limit']) +
    {
      seriesOverrides: [
        {
          alias: 'request',
          color: '#FFC000',
          fill: 0,
        },
        {
          alias: 'limit',
          color: '#E02F44',
          fill: 0,
        },
      ],
      tooltip: { sort: 2 },  // Sort descending.
    },
  containerCPUUsagePanel(title, containerName)::
    self.CPUUsagePanel(title, 'container=~"%s"' % containerName),

  memoryWorkingSetPanel(title, matcher)::
    $.panel(title) +
    $.queryPanel([
      // We use "max" instead of "sum" otherwise during a rolling update of a statefulset we will end up
      // summing the memory of the old pod (whose metric will be stale for 5m) to the new pod.
      'max by(pod) (container_memory_working_set_bytes{%s, %s})' % [$.namespaceMatcher(), matcher],
      'min(kube_pod_container_resource_requests{%s, %s, resource="memory"} > 0)' % [$.namespaceMatcher(), matcher],
      'min(container_spec_memory_limit_bytes{%s, %s} > 0)' % [$.namespaceMatcher(), matcher],
    ], ['{{pod}}', 'request', 'limit']) +
    {
      seriesOverrides: [
        {
          alias: 'request',
          color: '#FFC000',
          fill: 0,
        },
        {
          alias: 'limit',
          color: '#E02F44',
          fill: 0,
        },
      ],
      yaxes: $.yaxes('bytes'),
      tooltip: { sort: 2 },  // Sort descending.
    },
  containerMemoryWorkingSetPanel(title, containerName)::
    self.memoryWorkingSetPanel(title, 'container=~"%s"' % containerName),

  goHeapInUsePanel(title, jobName)::
    $.panel(title) +
    $.queryPanel(
      'sum by(%s) (go_memstats_heap_inuse_bytes{%s})' % [$._config.per_instance_label, $.jobMatcher(jobName)],
      '{{%s}}' % $._config.per_instance_label
    ) +
    {
      yaxes: $.yaxes('bytes'),
      tooltip: { sort: 2 },  // Sort descending.
    },

  filterNodeDisk(matcher)::
    |||
      ignoring(%s) group_right() (label_replace(count by(%s, %s, device) (container_fs_writes_bytes_total{%s, %s, device!~".*sda.*"}), "device", "$1", "device", "/dev/(.*)") * 0)
    ||| % [$._config.per_instance_label, $._config.per_node_label, $._config.per_instance_label, $.namespaceMatcher(), matcher],
  filterNodeDiskContainer(containerName)::
    self.filterNodeDisk('container="%s"' % containerName),

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
    $.panel(title) +
    $.queryPanel('max by(persistentvolumeclaim) (kubelet_volume_stats_used_bytes{%s} / kubelet_volume_stats_capacity_bytes{%s}) and count by(persistentvolumeclaim) (kube_persistentvolumeclaim_labels{%s,%s})' % [$.namespaceMatcher(), $.namespaceMatcher(), $.namespaceMatcher(), $.containerLabelMatcher(containerName)], '{{persistentvolumeclaim}}') +
    { yaxes: $.yaxes('percentunit') },
}
