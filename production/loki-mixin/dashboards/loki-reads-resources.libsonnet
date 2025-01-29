local selector = (import '../selectors.libsonnet').new;

local jobSelectors = {
  bloomGateway: selector().job('bloom-gateway').build(),
  cortexGateway: selector().job('cortex-gateway').build(),
  indexGateway: selector().job('index-gateway').build(),
  ingester: selector().job(['ingester', 'partition-ingester']).build(),
  queryFrontend: selector().job('query-frontend').build(),
  queryScheduler: selector().job('query-scheduler').build(),
  querier: selector().job('querier').build(),
  ruler: selector().job('ruler').build(),
};

local containerSelectors = {
  bloomGateway: selector().container('bloom-gateway').build(),
  cortexGateway: selector().container('cortex-gateway').build(),
  indexGateway: selector().container('index-gateway').build(),
  ingester: selector().container(['ingester', 'partition-ingester']).build(),
  queryFrontend: selector().container('query-frontend').build(),
  queryScheduler: selector().container('query-scheduler').build(),
  querier: selector().container('querier').build(),
  ruler: selector().container('ruler').build(),
};

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+:: if $._config.ssd.enabled then {} else {
    'loki-reads-resources.json':
      $.dashboard('Loki / Reads Resources', uid='reads-resources')
      .addCluster()
      .addNamespace()
      .addTag()
      .addRowIf(
        $._config.internal_components,
        $.row('Gateway')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.cortexGateway),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.cortexGateway),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.cortexGateway),
        )
      )
      .addRow(
        $.row('Query Frontend')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.queryFrontend),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.queryFrontend),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.queryFrontend),
        )
      )
      .addRow(
        $.row('Query Scheduler')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.queryScheduler),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.queryScheduler),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.queryScheduler),
        )
      )
      .addRow(
        $.row('Querier')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.querier),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.querier),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.querier),
        )
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_written_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.per_node_label, container_disk: $.filterNodeDisk(containerSelectors.querier) },
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_read_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.per_node_label, container_disk: $.filterNodeDisk(containerSelectors.querier) },
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'querier'),
        )
      )
      // Otherwise we add the index gateway information
      .addRow(
        $.row('Index Gateway')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.indexGateway),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.indexGateway),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.indexGateway),
        )
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_written_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.per_node_label, container_disk: $.filterNodeDisk(containerSelectors.indexGateway) },
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_read_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.per_node_label, container_disk: $.filterNodeDisk(containerSelectors.indexGateway) },
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'index-gateway'),
        )
      )
      .addRow(
        $.row('Bloom Gateway')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.bloomGateway),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.bloomGateway),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.bloomGateway),
        )
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_written_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.per_node_label, container_disk: $.filterNodeDisk(containerSelectors.bloomGateway) },
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_read_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.per_node_label, container_disk: $.filterNodeDisk(containerSelectors.bloomGateway) },
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', 'bloom-gateway'),
        )
      )
      .addRow(
        $.row('Ingester')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.ingester),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.ingester),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.ingester),
        )
      )
      .addRow(
        $.row('Ruler')
        .addPanel(
          $.newQueryPanel('Rules') +
          $.queryPanel(
            |||
              sum by(%(label)s) (
                loki_prometheus_rule_group_rules{%(matcher)s}
              )
              or
              sum by(%(label)s) (
                cortex_prometheus_rule_group_rules{%(matcher)s}
              )
            ||| % { label: $._config.per_instance_label, matcher: jobSelectors.ruler },
            '{{%s}}' % $._config.per_instance_label
          ),
        )
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.ruler),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.ruler),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.ruler),
        )
      ),
  },
}
