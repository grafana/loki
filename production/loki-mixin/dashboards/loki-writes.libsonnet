local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+: {
    'loki-writes.json':
      ($.dashboard('Loki / Writes'))
      .addClusterSelectorTemplates()
      .addRow(
        $.row('Frontend (cortex_gw)')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_request_duration_seconds_count{%s, route=~"api_prom_push|loki_api_v1_push"}' % $.jobMatcher($._config.job_names.gateway))
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            $.jobSelector($._config.job_names.gateway) + [utils.selector.re('route', 'api_prom_push|loki_api_v1_push')],
          )
        )
      )
      .addRow(
        $.row('Distributor')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_request_duration_seconds_count{%s}' % std.rstripChars($.jobMatcher($._config.job_names.distributor), ','))
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            $.jobSelector($._config.job_names.distributor)
          )
        )
      )
      .addRow(
        $.row('Ingester')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_request_duration_seconds_count{%s, route="/logproto.Pusher/Push"}' % $.jobMatcher($._config.job_names.ingester))
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            $.jobSelector($._config.job_names.ingester) + [utils.selector.eq('route', '/logproto.Pusher/Push')],
          )
        )
      )
      .addRow(
        $.row('BigTable')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('cortex_bigtable_request_duration_seconds_count{%s, operation="/google.bigtable.v2.Bigtable/MutateRows"}' % $.jobMatcher($._config.job_names.ingester))
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'cortex_bigtable_request_duration_seconds',
            $.jobSelector($._config.job_names.ingester) + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/MutateRows')]
          )
        )
      )
      .addRow(
        $.row('BoltDB Shipper')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s, operation="WRITE"}' % $.jobMatcher($._config.job_names.ingester))
        )
        .addPanel(
          $.panel('Latency') +
          $.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{%s, operation="WRITE"}' % $.jobMatcher($._config.job_names.ingester))
        )
      ),
  },
}
