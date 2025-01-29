local selector = (import '../selectors.libsonnet').new;

local jobSelectors = {
  cortexGateway: selector().job('cortex-gateway').build(),
  distributor: selector().job('distributor').build(),
  ingester: selector().job(['ingester', 'partition-ingester']).build(),
};

local containerSelectors = {
  cortexGateway: selector().container('cortex-gateway').build(),
  distributor: selector().container('distributor').build(),
  ingester: selector().container(['ingester', 'partition-ingester']).build(),
};

local podSelectors = {
  cortexGateway: selector().container('cortex-gateway').pod('cortex-gateway').build(),
  distributor: selector().container('distributor').pod('distributor').build(),
  ingester: selector().container(['ingester', 'partition-ingester']).pod(['ingester', 'partition-ingester']).build(),
};

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+:: if $._config.ssd.enabled then {} else {
    'loki-writes-resources.json':
      ($.dashboard('Loki / Writes Resources', uid='writes-resources'))
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
        $.row('Distributor')
        .addPanel(
          $.CPUUsagePanel('CPU', containerSelectors.distributor),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', containerSelectors.distributor),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.distributor),
        )
      )
      .addRow(
        $.row('Ingester')
        .addPanel(
          $.newQueryPanel('In-memory streams') +
          $.queryPanel(
            |||
              sum by(%s) (
                loki_ingester_memory_streams{%s}
              )
            ||| % [$._config.labels.per_instance, jobSelectors.ingester],
            '{{%s}}' % $._config.labels.per_instance
          ) +
          {
            tooltip: { sort: 2 },  // Sort descending.
          },
        )
        .addPanel(
          $.CPUUsagePanel('CPU', podSelectors.ingester),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', podSelectors.ingester),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.ingester),
        )
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            |||
              sum by(%s, device) (
                rate(node_disk_written_bytes_total[$__rate_interval])
              ) + %s
            ||| % [$._config.labels.node, $.filterNodeDisk(podSelectors.ingester)],
            '{{%s}} - {{device}}' % $._config.labels.per_instance
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            |||
              sum by(%s, device) (
                rate(node_disk_read_bytes_total[$__rate_interval])
              ) + %s
            ||| % [$._config.labels.node, $.filterNodeDisk(podSelectors.ingester)],
            '{{%s}} - {{device}}' % $._config.labels.per_instance
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', jobSelectors.ingester),
        )
      ),
  },
}
