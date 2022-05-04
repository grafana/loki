local grafana = import 'grafonnet/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+::
    {
      'loki-writes-resources.json':
        ($.dashboard('Loki / Writes Resources', uid='writes-resources'))
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
          grafana.row.new('Ingester')
          .addPanel(
            $.panel('In-memory streams') +
            $.queryPanel(
              'sum by(%s) (loki_ingester_memory_streams{%s})' % [$._config.per_instance_label, $.jobMatcher('ingester')],
              '{{%s}}' % $._config.per_instance_label
            ) +
            {
              tooltip: { sort: 2 },  // Sort descending.
            },
          )
          .addPanel(
            $.containerCPUUsagePanel('CPU', 'ingester'),
          )
          .addPanel(
            $.containerMemoryWorkingSetPanel('Memory (workingset)', 'ingester'),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', 'ingester'),
          )
          .addPanel(
            $.panel('Disk Writes') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('ingester')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.panel('Disk Reads') +
            $.queryPanel(
              'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDiskContainer('ingester')],
              '{{%s}} - {{device}}' % $._config.per_instance_label
            ) +
            $.stack +
            { yaxes: $.yaxes('Bps') },
          )
          .addPanel(
            $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'ingester'),
          )
        ),
    },
}
