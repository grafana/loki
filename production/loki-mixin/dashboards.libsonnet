local g = import 'grafana-builder/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+: {
    'loki-logs.json': import './dashboard-loki-logs.json',
    'loki-operational.json': import './dashboard-loki-operational.json',
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
      )
      .addRow(
        g.row('BigTable')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('cortex_bigtable_request_duration_seconds_count{cluster=~"$cluster", job=~"($namespace)/ingester", operation="/google.bigtable.v2.Bigtable/MutateRows"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('cortex_bigtable_request_duration_seconds', [utils.selector.re('cluster', '$cluster'), utils.selector.re('job', '($namespace)/ingester')] + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/MutateRows')])
        )
      )
      .addRow(
        g.row('BoltDB Shipper')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_boltdb_shipper_request_duration_seconds_count{cluster=~"$cluster", job=~"($namespace)/ingester", operation="WRITE"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{cluster=~"$cluster", job=~"($namespace)/ingester", operation="WRITE"}')
        )
      ),

    local http_routes = 'loki_api_v1_series|api_prom_series|api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_labels|loki_api_v1_label_name_values',
    local grpc_routes = '/logproto.Querier/Query|/logproto.Querier/Label|/logproto.Querier/Series',

    'loki-reads.json':
      g.dashboard('Loki / Reads')
      .addTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
      .addTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
      .addRow(
        g.row('Frontend (cortex_gw)')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/cortex-gw", route=~"%s"}' % http_routes)
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.eq('job', '$namespace/cortex-gw'), utils.selector.re('route', http_routes)], extra_selectors=[utils.selector.eq('cluster', '$cluster')], sum_by=['route'])
        )
      )
      .addRow(
        g.row('Frontend (query-frontend)')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/query-frontend", route=~"%s"}' % http_routes)
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.eq('job', '$namespace/query-frontend'), utils.selector.re('route', http_routes)], extra_selectors=[utils.selector.eq('cluster', '$cluster')], sum_by=['route'])
        )
      )
      .addRow(
        g.row('Querier')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/querier", route=~"%s"}' % http_routes)
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.eq('job', '$namespace/querier'), utils.selector.re('route', http_routes)], extra_selectors=[utils.selector.eq('cluster', '$cluster')], sum_by=['route'])
        )
      )
      .addRow(
        g.row('Ingester')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/ingester",route=~"%s"}' % grpc_routes)
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('loki_request_duration_seconds', [utils.selector.eq('job', '$namespace/ingester'), utils.selector.re('route', grpc_routes)], extra_selectors=[utils.selector.eq('cluster', '$cluster')], sum_by=['route'])
        )
      )
      .addRow(
        g.row('BigTable')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('cortex_bigtable_request_duration_seconds_count{cluster=~"$cluster", job=~"($namespace)/querier", operation="/google.bigtable.v2.Bigtable/ReadRows"}')
        )
        .addPanel(
          g.panel('Latency') +
          utils.latencyRecordingRulePanel('cortex_bigtable_request_duration_seconds', [utils.selector.re('cluster', '$cluster'), utils.selector.re('job', '($namespace)/querier')] + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/ReadRows')])
        )
      )
      .addRow(
        g.row('BoltDB Shipper')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_boltdb_shipper_request_duration_seconds_count{cluster=~"$cluster", job=~"($namespace)/querier", operation="QUERY"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{cluster=~"$cluster", job=~"($namespace)/querier", operation="QUERY"}')
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
          g.queryPanel('cortex_ingester_flush_queue_length{cluster="$cluster", job="$namespace/ingester"}', '{{pod}}'),
        )
        .addPanel(
          g.panel('Flush Rate') +
          g.qpsPanel('loki_ingester_chunk_age_seconds_count{cluster="$cluster", job="$namespace/ingester"}'),
        ),
      )
      .addRow(
        g.row('Duration')
        .addPanel(
          g.panel('Chunk Duration hours (end-start)') +
          g.queryPanel(
            [
              'histogram_quantile(0.5, sum(rate(loki_ingester_chunk_bounds_hours_bucket{cluster="$cluster", job="$namespace/ingester"}[5m])) by (le))',
              'histogram_quantile(0.99, sum(rate(loki_ingester_chunk_bounds_hours_bucket{cluster="$cluster", job="$namespace/ingester"}[5m])) by (le))',
              'sum(rate(loki_ingester_chunk_bounds_hours_sum{cluster="$cluster", job="$namespace/ingester"}[5m])) / sum(rate(loki_ingester_chunk_bounds_hours_count{cluster="$cluster", job="$namespace/ingester"}[5m]))',
            ],
            [
              'p50',
              'p99',
              'avg',
            ],
          ),
        )
      ),
  },
}
