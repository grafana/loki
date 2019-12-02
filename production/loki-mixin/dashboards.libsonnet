local g = import 'grafana-builder/grafana.libsonnet';
local utils = import "mixin-utils/utils.libsonnet";

{
  dashboards+: {
    'loki-writes.json':
      g.dashboard('Loki / Writes')
      .addMultiTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
      .addMultiTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
      .addRow(
        g.row('Frontend (cortex_gw)')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster=~"$cluster", job=~"($namespace)/cortex-gw", route=~"api_prom_push|loki_api_v1_push"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.re('job', '($namespace)/cortex-gw'), utils.selector.re('route', 'api_prom_push|loki_api_v1_push')], extra_selectors=[utils.selector.re('cluster', '$cluster')])
        )
      )
      .addRow(
        g.row('Distributor')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster=~"($cluster)", job=~"($namespace)/distributor"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.re('job', '($namespace)/distributor')], extra_selectors=[utils.selector.re('cluster', '$cluster')])
        )
      )
      .addRow(
        g.row('Ingester')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster=~"$cluster", job=~"($namespace)/ingester",route="/logproto.Pusher/Push"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.re('job', '($namespace)/ingester'), utils.selector.eq('route', '/logproto.Pusher/Push')], extra_selectors=[utils.selector.re('cluster', '$cluster')])
        )
      ),

    'loki-reads.json':
      g.dashboard('Loki / Reads')
      .addTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
      .addTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
      .addRow(
        g.row('Frontend (cortex_gw)')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/cortex-gw", route=~"api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.eq('job', '$namespace/cortex-gw'), utils.selector.re('route', 'api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values')], extra_selectors=[utils.selector.eq('cluster', '$cluster')], sum_by=['route'])
        )
      )
      .addRow(
        g.row('Querier')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/querier", route=~"api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.eq('job', '$namespace/querier'), utils.selector.re('route', 'api_prom_query|api_prom_labels|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_label|loki_api_v1_label_name_values')], extra_selectors=[utils.selector.eq('cluster', '$cluster')], sum_by=['route'])
        )
      )
      .addRow(
        g.row('Ingester')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/ingester",route=~"/logproto.Querier/Query|/logproto.Querier/Label"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.eq('job', '$namespace/ingester'), utils.selector.re('route', '/logproto.Querier/Query|/logproto.Querier/Label')], extra_selectors=[utils.selector.eq('cluster', '$cluster')], sum_by=['route'])
        )
      ),


    'loki-chunks.json':
      g.dashboard('Loki / Chunks')
      .addTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
      .addTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
      .addRow(
        g.row('Active Series / Chunks')
        .addPanel(
          g.panel('Series') +
          g.queryPanel('sum(loki_ingester_memory_chunks{cluster="$cluster", job="$namespace/ingester"})', 'series'),
        )
        .addPanel(
          g.panel('Chunks per series') +
          g.queryPanel('sum(loki_ingester_memory_chunks{cluster="$cluster", job="$namespace/ingester"}) / sum(loki_ingester_memory_streams{job="$namespace/ingester"})', 'chunks'),
        )
      )
      .addRow(
        g.row('Flush Stats')
        .addPanel(
          g.panel('Utilization') +
          g.latencyPanel('loki_ingester_chunk_utilization', '{cluster="$cluster", job="$namespace/ingester"}', multiplier='1') +
          { yaxes: g.yaxes('percentunit') },
        )
        .addPanel(
          g.panel('Age') +
          g.latencyPanel('loki_ingester_chunk_age_seconds', '{cluster="$cluster", job="$namespace/ingester"}'),
        ),
      )
      .addRow(
        g.row('Flush Stats')
        .addPanel(
          g.panel('Size') +
          g.latencyPanel('loki_ingester_chunk_entries', '{cluster="$cluster", job="$namespace/ingester"}', multiplier='1') +
          { yaxes: g.yaxes('short') },
        )
        .addPanel(
          g.panel('Entries') +
          g.queryPanel('sum(rate(cortex_chunk_store_index_entries_per_chunk_sum{cluster="$cluster", job="$namespace/ingester"}[5m])) / sum(rate(cortex_chunk_store_index_entries_per_chunk_count{cluster="$cluster", job="$namespace/ingester"}[5m]))', 'entries'),
        ),
      )
      .addRow(
        g.row('Flush Stats')
        .addPanel(
          g.panel('Queue Length') +
          g.queryPanel('cortex_ingester_flush_queue_length{cluster="$cluster", job="$namespace/ingester"}', '{{instance}}'),
        )
        .addPanel(
          g.panel('Flush Rate') +
          g.qpsPanel('loki_ingester_chunk_age_seconds_count{cluster="$cluster", job="$namespace/ingester"}'),
        ),
      ),

      'promtail.json':
        g.dashboard('Loki / Promtail')
        .addTemplate('cluster', 'kube_pod_container_info{image=~".*promtail.*"}', 'cluster')
        .addTemplate('namespace', 'kube_pod_container_info{image=~".*promtail.*"}', 'namespace')
        .addRow(
          g.row('Targets & Files')
          .addPanel(
            g.panel('Active Targets') +
            g.queryPanel(
              'sum(promtail_targets_active_total{cluster="$cluster", job="$namespace/promtail"})',
              'Active Targets',
            ),
           )
           .addPanel(
            g.panel('Active Files') +
             g.queryPanel(
              'sum(promtail_files_active_total{cluster="$cluster", job="$namespace/promtail"})',
              'Active Targets',
            ),
          )
        )
        .addRow(
          g.row('IO')
          .addPanel(
            g.panel('Bps') +
            g.queryPanel(
              'sum(rate(promtail_read_bytes_total{cluster="$cluster", job="$namespace/promtail"}[1m]))',
              'logs read',
            ) +
            { yaxes: g.yaxes('Bps') },
           )
           .addPanel(
            g.panel('Lines') +
             g.queryPanel(
              'sum(rate(promtail_read_lines_total{cluster="$cluster", job="$namespace/promtail"}[1m]))',
              'lines read',
            ),
          )
        )
        .addRow(
          g.row('Requests')
          .addPanel(
            g.panel('QPS') +
            g.qpsPanel('promtail_request_duration_seconds_count{cluster="$cluster", job="$namespace/promtail"}')
           )
           .addPanel(
             g.panel('Latency') +
             utils.latencyRecordingRulePanel('promtail_request_duration_seconds', [utils.selector.eq('job', '$namespace/promtail')], extra_selectors=[utils.selector.eq('cluster', '$cluster')])
           )
        )
  },
}
