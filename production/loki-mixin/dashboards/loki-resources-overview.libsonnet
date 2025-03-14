(import 'dashboard-utils.libsonnet') {
  local read_pod_matcher = 'container="loki", pod=~"%s-read.*"' % $._config.ssd.pod_prefix_matcher,
  local read_job_matcher = '%s-read' % $._config.ssd.pod_prefix_matcher,

  local write_pod_matcher = 'container="loki", pod=~"%s-write.*"' % $._config.ssd.pod_prefix_matcher,
  local write_job_matcher = '%s-write' % $._config.ssd.pod_prefix_matcher,

  local backend_pod_matcher = 'container="loki", pod=~"%s-backend.*"' % $._config.ssd.pod_prefix_matcher,
  local backend_job_matcher = '%s-backend' % $._config.ssd.pod_prefix_matcher,

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
          $.CPUUsagePanel('CPU', read_pod_matcher),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', read_pod_matcher),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', read_job_matcher),
        )
      )
      .addRow(
        $.row('Write path')
        .addPanel(
          $.newQueryPanel('In-memory streams') +
          $.queryPanel(
            'sum by(%s) (loki_write_memory_streams{%s})' % [$._config.per_instance_label, $.jobMatcher(write_job_matcher)],
            '{{%s}}' % $._config.per_instance_label
          ) +
          {
            tooltip: { sort: 2 },  // Sort descending.
          }
        )
        .addPanel(
          $.CPUUsagePanel('CPU', write_pod_matcher),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', write_pod_matcher),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', write_job_matcher),
        )
      )
      .addRow(
        $.row('')
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            'sum by(%s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $.filterNodeDisk(write_pod_matcher)],
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            'sum by(%s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $.filterNodeDisk(write_pod_matcher)],
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', write_job_matcher),
        )
      )
      .addRow(
        $.row('Backend path')
        .addPanel(
          $.CPUUsagePanel('CPU', backend_pod_matcher),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', backend_pod_matcher),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', backend_job_matcher),
        )
      )
      .addRow(
        $.row('')
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            'sum by(%s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $.filterNodeDisk(backend_pod_matcher)],
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            'sum by(%s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $.filterNodeDisk(backend_pod_matcher)],
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.containerDiskSpaceUtilizationPanel('Disk Space Utilization', backend_job_matcher),
        )
      ),
  },
}
