local g = import 'grafana-builder/grafana.libsonnet';

{
  dashboards+: {
    'loki-writes.json':
      g.dashboard('Loki / Writes')
      .addTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
      .addTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
      .addRow(
        g.row('Frontend (cortex_gw)')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('cortex_gw_request_duration_seconds_count{cluster="$cluster", job="$namespace/cortex-gw", route="cortex-write"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyRecordingRulePanel('cortex_gw_request_duration_seconds', [g.selector.eq('job', '$namespace/cortex-gw'), g.selector.eq('route', 'cortex-write')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
        )
      )
      .addRow(
        g.row('Distributor')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/distributor", route="api_prom_push"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyRecordingRulePanel('loki_request_duration_seconds', [g.selector.eq('job', '$namespace/distributor'), g.selector.eq('route', 'api_prom_push')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
        )
      )
      .addRow(
        g.row('Ingester')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/ingester",route="/logproto.Pusher/Push"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyRecordingRulePanel('loki_request_duration_seconds', [g.selector.eq('job', '$namespace/ingester'), g.selector.eq('route', '/logproto.Pusher/Push')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
        )
      ),

    'loki-reads.json':
      g.dashboard('loki / Reads')
      .addTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
      .addTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
      .addRow(
        g.row('Frontend (cortex_gw)')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('cortex_gw_request_duration_seconds_count{cluster="$cluster", job="$namespace/cortex-gw", route="cortex-read"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyRecordingRulePanel('cortex_gw_request_duration_seconds', [g.selector.eq('job', '$namespace/cortex-gw'), g.selector.eq('route', 'cortex-read')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
        )
      )
      .addRow(
        g.row('Querier')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/querier"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyRecordingRulePanel('loki_request_duration_seconds', [g.selector.eq('job', '$namespace/querier')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
        )
      )
      .addRow(
        g.row('Ingester')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('loki_request_duration_seconds_count{cluster="$cluster", job="$namespace/ingester",route!~"/logproto.Pusher/Push|metrics|ready|traces"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyRecordingRulePanel('loki_request_duration_seconds', [g.selector.eq('job', '$namespace/ingester'), g.selector.nre('route', '/logproto.Pusher/Push|metrics|ready')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
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
          g.queryPanel('sum(loki_ingester_memory_chunks{cluster="$cluster", job="$namespace/ingester"}) / sum(loki_ingester_memory_series{job="$namespace/ingester"})', 'chunks'),
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
          g.latencyPanel('loki_ingester_chunk_length', '{cluster="$cluster", job="$namespace/ingester"}', multiplier='1') +
          { yaxes: g.yaxes('short') },
        )
        .addPanel(
          g.panel('Entries') +
          g.queryPanel('sum(rate(loki_chunk_store_index_entries_per_chunk_sum{cluster="$cluster", job="$namespace/ingester"}[5m])) / sum(rate(loki_chunk_store_index_entries_per_chunk_count{cluster="$cluster", job="$namespace/ingester"}[5m]))', 'entries'),
        ),
      )
      .addRow(
        g.row('Flush Stats')
        .addPanel(
          g.panel('Queue Length') +
          g.queryPanel('loki_ingester_flush_queue_length{cluster="$cluster", job="$namespace/ingester"}', '{{instance}}'),
        )
        .addPanel(
          g.panel('Flush Rate') +
          g.qpsPanel('loki_ingester_chunk_age_seconds_count{cluster="$cluster", job="$namespace/ingester"}'),
        ),
      ),

    'loki-frontend.json':
      g.dashboard('Loki / Frontend')
      .addTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
      .addTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
      .addRow(
        g.row('loki Reqs (cortex_gw)')
        .addPanel(
          g.panel('QPS') +
          g.qpsPanel('cortex_gw_request_duration_seconds_count{cluster="$cluster", job="$namespace/cortex-gw"}')
        )
        .addPanel(
          g.panel('Latency') +
          g.latencyRecordingRulePanel('cortex_gw_request_duration_seconds', [g.selector.eq('job', '$namespace/cortex-gw')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
        )
      ),
      'promtail.json':
        g.dashboard('Loki / Promtail')
        .addTemplate('cluster', 'kube_pod_container_info{image=~".*loki.*"}', 'cluster')
        .addTemplate('namespace', 'kube_pod_container_info{image=~".*loki.*"}', 'namespace')
        .addRow(
          g.row('promtail Reqs')
          .addPanel(
            g.panel('QPS') +
            g.qpsPanel('promtail_request_duration_seconds_count{cluster="$cluster", job="$namespace/promtail"}')
           )
           .addPanel(
             g.panel('Latency') +
             g.latencyRecordingRulePanel('promtail_request_duration_seconds', [g.selector.eq('job', '$namespace/promtail')], extra_selectors=[g.selector.eq('cluster', '$cluster')])
           )
        )
  },
}
