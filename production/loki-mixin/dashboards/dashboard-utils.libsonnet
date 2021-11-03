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
      addLog(name='logs'):: self {
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
          d.addMultiTemplate('cluster', 'loki_build_info', 'cluster')
          .addMultiTemplate('namespace', 'loki_build_info{cluster=~"$cluster"}', 'namespace')
        else
          d.addTemplate('cluster', 'loki_build_info', 'cluster')
          .addTemplate('namespace', 'loki_build_info{cluster=~"$cluster"}', 'namespace'),
    },

  jobMatcher(job)::
    'cluster=~"$cluster", job=~"($namespace)/%s"' % job,

  namespaceMatcher()::
    'cluster=~"$cluster", namespace=~"$namespace"',
  logPanel(title, selector, datasource='$logs'):: {
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
  containerCPUUsagePanel(title, containerName)::
    $.panel(title) +
    $.queryPanel([
      'sum by(pod) (rate(container_cpu_usage_seconds_total{%s,container="%s"}[$__rate_interval]))' % [$.namespaceMatcher(), containerName],
      'min(container_spec_cpu_quota{%s,container="%s"} / container_spec_cpu_period{%s,container="%s"})' % [$.namespaceMatcher(), containerName, $.namespaceMatcher(), containerName],
    ], ['{{pod}}', 'limit']) +
    {
      seriesOverrides: [
        {
          alias: 'limit',
          color: '#E02F44',
          fill: 0,
        },
      ],
      tooltip: { sort: 2 },  // Sort descending.
    },

  containerMemoryWorkingSetPanel(title, containerName)::
    $.panel(title) +
    $.queryPanel([
      // We use "max" instead of "sum" otherwise during a rolling update of a statefulset we will end up
      // summing the memory of the old pod (whose metric will be stale for 5m) to the new pod.
      'max by(pod) (container_memory_working_set_bytes{%s,container="%s"})' % [$.namespaceMatcher(), containerName],
      'min(container_spec_memory_limit_bytes{%s,container="%s"} > 0)' % [$.namespaceMatcher(), containerName],
    ], ['{{pod}}', 'limit']) +
    {
      seriesOverrides: [
        {
          alias: 'limit',
          color: '#E02F44',
          fill: 0,
        },
      ],
      yaxes: $.yaxes('bytes'),
      tooltip: { sort: 2 },  // Sort descending.
    },

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

  filterNodeDiskContainer(containerName)::
    |||
      ignoring(%s) group_right() (label_replace(count by(%s, %s, device) (container_fs_writes_bytes_total{%s,container="%s",device!~".*sda.*"}), "device", "$1", "device", "/dev/(.*)") * 0)
    ||| % [$._config.per_instance_label, $._config.per_node_label, $._config.per_instance_label, $.namespaceMatcher(), containerName],
}
