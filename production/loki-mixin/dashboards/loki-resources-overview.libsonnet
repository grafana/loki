local selector = (import '../selectors.libsonnet').new;

local podSelectors = {
  read: selector().label('container').eq('loki').pod('read').build(),
  write: selector().label('container').eq('loki').pod('write').build(),
  backend: selector().label('container').eq('loki').pod('backend').build(),
};

local jobSelectors = {
  read: selector().job('read').build(),
  write: selector().job('write').build(),
  backend: selector().job('backend').build(),
};

(import 'dashboard-utils.libsonnet') {

  // This dashboard is for the single scalable deployment only and it :
  // - replaces the loki-reads-resources dashboards
  // - replaces the loki-write-resources dashboards
  // - adds backend pods resources
  grafanaDashboards+:: if !$._config.ssd.enabled then {} else {
    'loki-resources-overview.json':
      ($.dashboard('Loki / Resources Overview', uid='resources-overview'))
      .addCluster()
      .addNamespace()
      .addTag()
      .addRow(
        // The read path does not display disk utilization as the index gateway is present in the backend pods.
        $.row('Read path')
        .addPanel(
          $.CPUUsagePanel('CPU', podSelectors.read),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', podSelectors.read),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.read),
        )
      )
      .addRow(
        $.row('Write path')
        .addPanel(
          $.newQueryPanel('In-memory streams') +
          $.queryPanel(
            |||
              sum by(%s) (
                loki_write_memory_streams{%s}
              )
            ||| % [$._config.labels.per_instance, jobSelectors.write],
            '{{%s}}' % $._config.labels.per_instance
          ) +
          {
            tooltip: { sort: 2 },  // Sort descending.
          }
        )
        .addPanel(
          $.CPUUsagePanel('CPU', podSelectors.write),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', podSelectors.write),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.write),
        )
      )
      .addRow(
        $.row('')
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_written_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.labels.node, container_disk: $.filterNodeDisk(podSelectors.write) },
            '{{%s}} - {{device}}' % $._config.labels.per_instance
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
            ||| % { per_node_label: $._config.labels.node, container_disk: $.filterNodeDisk(podSelectors.write) },
            '{{%s}} - {{device}}' % $._config.labels.per_instance
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', podSelectors.write),
        )
      )
      .addRow(
        $.row('Backend path')
        .addPanel(
          $.CPUUsagePanel('CPU', podSelectors.backend),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', podSelectors.backend),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.backend),
        )
      )
      .addRow(
        $.row('')
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            |||
              sum by(%(per_node_label)s, device) (
                rate(node_disk_written_bytes_total[$__rate_interval])
              )
              +
              %(container_disk)s
            ||| % { per_node_label: $._config.labels.node, container_disk: $.filterNodeDisk(podSelectors.backend) },
            '{{%s}} - {{device}}' % $._config.labels.per_instance
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
            ||| % { per_node_label: $._config.labels.node, container_disk: $.filterNodeDisk(podSelectors.backend) },
            '{{%s}} - {{device}}' % $._config.labels.per_instance
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', podSelectors.backend),
        )
      ),
  },
}
