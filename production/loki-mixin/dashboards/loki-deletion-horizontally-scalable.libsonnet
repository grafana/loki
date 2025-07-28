local vendor_config = import 'github.com/grafana/mimir/operations/mimir-mixin/config.libsonnet';
local vendor_utils = import 'github.com/grafana/mimir/operations/mimir-mixin/dashboards/dashboard-utils.libsonnet';
local g = import 'grafana-builder/grafana.libsonnet';
local grafana = import 'grafonnet/grafana.libsonnet';

{
  local worker_pod_matcher = if $._config.meta_monitoring.enabled
  then $._config.per_instance_label + '=~".*compactor-worker.*"'
  else 'container="compactor-worker"',
  local worker_job_matcher = if $._config.meta_monitoring.enabled
  then '".*compactor-worker"' % $._config.ssd.pod_prefix_matcher
  else 'compactor-worker',

  local compactor_pod_matcher = if $._config.meta_monitoring.enabled
  then $._config.per_instance_label + '=~"(.*compactor.*|%s-backend.*|loki-single-binary)"' % $._config.ssd.pod_prefix_matcher
  else 'container="compactor"',
  local compactor_job_matcher = if $._config.meta_monitoring.enabled
  then '"(.*compactor|%s-backend.*|loki-single-binary)"' % $._config.ssd.pod_prefix_matcher
  else 'compactor',

  _config+:: {
    horizontally_scalable_compactor_enabled: false,
  },
  grafanaDashboards+: if !$._config.horizontally_scalable_compactor_enabled then {} else {
    local dashboard = (
      vendor_utils {
        _config:: vendor_config._config + $._config {
          product: 'Loki',
          dashboard_prefix: 'Loki / ',
          tags: ['loki'],
        },
      }
    ),
    'loki-deletion-horizontally-scalable.json':
      // The dashboard() function automatically adds the "Loki / " prefix to the dashboard title.
      // This logic is inherited from mimir-mixin.
      dashboard.dashboard('Deletion(Horizontally Scalable)')
      // We can't make use of simplified template selectors from the loki dashboard utils until we port the cortex dashboard utils panel/grid functionality.
      .addTemplate('cluster', 'loki_build_info', $._config.per_cluster_label)
      .addTemplate('namespace', 'loki_build_info{' + $._config.per_cluster_label + '=~"$cluster"}', 'namespace')
      + {
        // This dashboard uses the new grid system in order to place panels (using gridPos).
        // Because of this we can't use the mixin's addRow() and addPanel().
        schemaVersion: 27,
        rows: null,
        // ugly hack, copy pasta the tag/link
        // code from the loki-mixin
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
        panels: [
          { type: 'row', title: 'Headline' },
          dashboard.panel('Num Worker Replicas') +
          dashboard.newStatPanel('count(loki_build_info{%s})' % $.jobMatcher('compactor-worker'), instant=true, unit='short', decimals=null, thresholds=[
            { color: 'red', value: null },
            { color: 'green', value: 1 },
          ]) +
          { gridPos: { h: 3, w: 6, x: 0, y: 1 } },

          dashboard.panel('Connected Workers') +
          $.newStatPanel('count(loki_compactor_worker_connected_to_compactor{%s} == 1) / count(loki_build_info{%s})' % [$.jobMatcher('compactor-worker'), $.jobMatcher('compactor-worker')], instant=true, unit='percentunit', decimals=null, thresholds=[
            { color: 'red', value: null },
            { color: 'green', value: 1 },
          ]) +
          { gridPos: { h: 3, w: 6, x: 6, y: 1 } },

          dashboard.panel('Num Manifests left to process') +
          dashboard.newStatPanel('loki_compactor_job_builder_num_manifests_left_to_process{%s}' % $.jobMatcher('compactor'), unit='short', decimals=null, thresholds=[
            { color: 'green', value: null },
          ]) +
          { gridPos: { h: 3, w: 6, x: 12, y: 1 } },

          dashboard.panel('Num Segments left to process') +
          dashboard.newStatPanel('loki_compactor_job_builder_num_segments_left_to_process{%s}' % $.jobMatcher('compactor'), unit='short', decimals=null, thresholds=[
            { color: 'green', value: null },
          ]) +
          { gridPos: { h: 3, w: 6, x: 18, y: 1 } },

          {
            type: 'row',
            title: 'Pending Delete Requests',
            collapsed: true,
            gridPos: { h: 1, w: 24, x: 0, y: 4 },
            panels: [
              dashboard.panel('Number of Pending Requests') +
              dashboard.newStatPanel('sum(loki_compactor_pending_delete_requests_count{%s})' % $.namespaceMatcher(), unit='short', decimals=null) +
              { gridPos: { h: 4, w: 12, x: 0, y: 5 } },

              dashboard.panel('Oldest Pending Request Age') +
              dashboard.newStatPanel('max(loki_compactor_oldest_pending_delete_request_age_seconds{%s})' % $.namespaceMatcher(), unit='dtdurations', decimals=null) +
              { gridPos: { h: 4, w: 12, x: 12, y: 5 } },
            ],
          },

          { type: 'row', title: 'Worker Resource Usage', gridPos: { h: 1, w: 24, x: 0, y: 5 } },
          $.CPUUsagePanel('CPU', worker_pod_matcher) + { gridPos: { h: 7, w: 8, x: 0, y: 6 } },
          $.memoryWorkingSetPanel('Memory (workingset)', worker_pod_matcher) + { gridPos: { h: 7, w: 8, x: 8, y: 6 } },
          $.goHeapInUsePanel('Memory (go heap inuse)', worker_job_matcher) + { gridPos: { h: 7, w: 8, x: 16, y: 6 } },

          {
            type: 'row',
            title: 'Compactor Resource Usage',
            collapsed: true,
            gridPos: { h: 1, w: 24, x: 0, y: 13 },
            panels: [
              $.CPUUsagePanel('CPU', compactor_pod_matcher) + { gridPos: { h: 7, w: 8, x: 0, y: 14 } },
              $.memoryWorkingSetPanel('Memory (workingset)', compactor_pod_matcher) + { gridPos: { h: 7, w: 8, x: 8, y: 14 } },
              $.goHeapInUsePanel('Memory (go heap inuse)', compactor_job_matcher) + { gridPos: { h: 7, w: 8, x: 16, y: 14 } },
            ],
          },

          { type: 'row', title: 'Manifest building', gridPos: { h: 1, w: 24, x: 0, y: 14 } },
          dashboard.panel('Manifest build attempts') +
          dashboard.queryPanel(['sum by (status) (increase(loki_compactor_manifest_build_attempts_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], ['{{status}}']) +
          { gridPos: { h: 7, w: 12, x: 0, y: 15 } },
          dashboard.panel('Chunks added to manifest for processing') +
          dashboard.queryPanel(['sum by (status) (increase(loki_compactor_manifest_chunks_selected_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], 'chunks count') +
          { gridPos: { h: 7, w: 12, x: 12, y: 15 } },

          { type: 'row', title: 'Jobs', gridPos: { h: 1, w: 24, x: 0, y: 22 } },
          dashboard.panel('Rate of jobs sent to worker for processing') +
          dashboard.queryPanel(['sum(rate(loki_compactor_jobs_queued_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], 'job creation rate') +
          { gridPos: { h: 7, w: 12, x: 0, y: 23 } },
          dashboard.panel('Rate of job processed by status') +
          dashboard.queryPanel(['sum by (status) (rate(loki_compactor_worker_jobs_processed_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor-worker')], ['{{status}}']) +
          { gridPos: { h: 7, w: 12, x: 12, y: 23 } },
          dashboard.panel('Rate of job retries by reason') +
          dashboard.queryPanel(['sum by (reason) (rate(loki_compactor_job_retries_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], ['{{reason}}']) +
          { gridPos: { h: 7, w: 12, x: 0, y: 30 } },
          dashboard.panel('Rate of dropped jobs due to running out of max attempts') +
          dashboard.queryPanel(['sum(rate(loki_compactor_jobs_dropped_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], 'jobs drop rate') +
          { gridPos: { h: 7, w: 12, x: 12, y: 30 } },
          dashboard.panel('Latency in processing of jobs') +
          dashboard.queryPanel([
            'histogram_quantile(0.99, sum by(le) (rate(loki_compactor_jobs_processing_duration_seconds_bucket{%s}[$__rate_interval])))' % $.jobMatcher('compactor'),
            'histogram_quantile(0.50, sum by(le) (rate(loki_compactor_jobs_processing_duration_seconds_bucket{%s}[$__rate_interval])))' % $.jobMatcher('compactor'),
          ], ['p95', 'p50']) +
          { gridPos: { h: 7, w: 12, x: 0, y: 37 } },
          dashboard.panel('Rate of dropped jobs due to running out of max attempts') +
          dashboard.queryPanel(['sum(rate(loki_compactor_jobs_dropped_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], 'jobs drop rate') +
          { gridPos: { h: 7, w: 12, x: 12, y: 37 } },
          dashboard.panel('Rate of lines deleted / sec') +
          dashboard.queryPanel(['sum(rate(loki_compactor_deletion_job_runner_deleted_lines_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor-worker')], 'log lines deletion rate') +
          { gridPos: { h: 7, w: 24, x: 0, y: 44 } },

          { type: 'row', title: 'Manifest Processing', gridPos: { h: 1, w: 24, x: 0, y: 51 } },
          dashboard.panel('Rate in failures at various manifest processing stages') +
          dashboard.queryPanel(['sum by (stage) (rate(loki_compactor_process_manifest_failures_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], ['{{stage}}']) +
          { gridPos: { h: 7, w: 12, x: 0, y: 52 } },
          dashboard.panel('Storage updates applied') +
          dashboard.queryPanel(['sum by (type) (rate(loki_compactor_deletion_storage_updates_applied_total{%s}[$__rate_interval]))' % $.jobMatcher('compactor')], ['{{type}}']) +
          { gridPos: { h: 7, w: 12, x: 12, y: 52 } },

          { type: 'row', title: 'Logs', gridPos: { h: 1, w: 24, x: 0, y: 59 } },
          $.logPanel('Worker Logs', '{%s}' % $.jobMatcher(worker_job_matcher)) +
          { gridPos: { h: 7, w: 24, x: 0, y: 60 } },
          $.logPanel('Compactor Logs', '{%s} != "OpenTelemetry" != "no marks file" != "compact" != "count=" != "ingester" != "skipping upload" != "retention" != "memberlist" != "uploading delete requests db"' % $.jobMatcher(compactor_job_matcher)) +
          { gridPos: { h: 7, w: 24, x: 0, y: 67 } },
        ],
      },
  },
}
