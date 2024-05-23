local grafana = import 'grafonnet/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local backend_pod_matcher = 'container="loki", pod=~"%s-backend.*"' % $._config.ssd.pod_prefix_matcher,
  local backend_job_matcher = '%s-backend' % $._config.ssd.pod_prefix_matcher,

  grafanaDashboards+:: if !$._config.ssd.enabled then {} else {
    'loki-backends-resources.json':
      $.dashboard('Loki / Backend Resources', uid='backends-resources')
      .addCluster()
      .addNamespace()
      .addTag()
      .addRow(
        grafana.row.new('Backend path')
        .addPanel(
          $.CPUUsagePanel('CPU', backend_pod_matcher),
        )
        .addPanel(
          $.memoryWorkingSetPanel('Memory (workingset)', backend_pod_matcher),
        )
        .addPanel(
          $.goHeapInUsePanel('Memory (go heap inuse)', backend_job_matcher),
        )
        .addPanel(
          $.newQueryPanel('Disk Writes', 'Bps') +
          $.queryPanel(
            'sum by(%s, %s, device) (rate(node_disk_written_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(backend_pod_matcher)],
            '{{%s}} - {{device}}' % $._config.per_instance_label
          ) +
          $.withStacking,
        )
        .addPanel(
          $.newQueryPanel('Disk Reads', 'Bps') +
          $.queryPanel(
            'sum by(%s, %s, device) (rate(node_disk_read_bytes_total[$__rate_interval])) + %s' % [$._config.per_node_label, $._config.per_instance_label, $.filterNodeDisk(backend_pod_matcher)],
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
