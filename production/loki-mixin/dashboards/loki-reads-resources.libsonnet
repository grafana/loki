local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+:
    {
      'loki-reads-resources.json':
        ($.dashboard('Loki / Reads Resources', uid='reads-resources'))
        .addCluster()
        .addNamespace()
        .addTag()
        .addRow(
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
        .addRow(
          $.row('Query Frontend')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'query-frontend'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'query-frontend'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'query-frontend'),
          )
        )
        .addRow(
          $.row('Querier')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'querier'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'querier'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'querier'),
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.panel('Disk Writes') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('querier')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.panel('Disk Reads') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('querier')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'querier'),
          )
        )
        .addRow(
          $.row('Index Gateway')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'index-gateway'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'index-gateway'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'index-gateway'),
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.panel('Disk Writes') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('index-gateway')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.panel('Disk Reads') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('index-gateway')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'index-gateway'),
          )
        )
        .addRow(
          $.row('Ingester')
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'ingester'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'ingester'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'ingester'),
          )
        )
        .addRow(
          $.row('Ruler')
          .addPanel(
            $.panel('Rules') +
            $.queryPanel(
              'sum by(%s) (cortex_prometheus_rule_group_rules{%s})' % [$._config.per_instance_label, $.jobMatcher('ruler')],
              '{{%s}}' % $._config.per_instance_label
            ),
          )
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'ruler'),
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'ruler'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'ruler'),
          )
        ),
    },
}
