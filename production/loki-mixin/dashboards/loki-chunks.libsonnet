local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+: {
    'loki-chunks.json':
      ($.dashboard('Loki / Chunks'))
      .addClusterSelectorTemplates(false)
      .addRow(
        $.row('Active Series / Chunks')
        .addPanel(
          $.panel('Series') +
          $.queryPanel('sum(loki_ingester_memory_chunks{%s})' % $.jobMatcher($._config.job_names.ingester), 'series'),
        )
        .addPanel(
          $.panel('Chunks per series') +
          $.queryPanel(
            'sum(loki_ingester_memory_chunks{%s}) / sum(loki_ingester_memory_streams{job=~"%s"})' % [
              $.jobMatcher($._config.job_names.ingester),
              $.jobMatcher($._config.job_names.ingester),
            ],
            'chunks'
          ),
        )
      )
      .addRow(
        $.row('Flush Stats')
        .addPanel(
          $.panel('Utilization') +
          $.latencyPanel('loki_ingester_chunk_utilization', '{%s}' % $.jobMatcher($._config.job_names.ingester), multiplier='1') +
          { yaxes: $.yaxes('percentunit') },
        )
        .addPanel(
          $.panel('Age') +
          $.latencyPanel('loki_ingester_chunk_age_seconds', '{%s}' % $.jobMatcher($._config.job_names.ingester)),
        ),
      )
      .addRow(
        $.row('Flush Stats')
        .addPanel(
          $.panel('Size') +
          $.latencyPanel('loki_ingester_chunk_entries', '{%s}' % $.jobMatcher($._config.job_names.ingester), multiplier='1') +
          { yaxes: $.yaxes('short') },
        )
        .addPanel(
          $.panel('Entries') +
          $.queryPanel(
            'sum(rate(loki_chunk_store_index_entries_per_chunk_sum{%s}[5m])) / sum(rate(loki_chunk_store_index_entries_per_chunk_count{%s}[5m]))' % [
              $.jobMatcher($._config.job_names.ingester),
              $.jobMatcher($._config.job_names.ingester),
            ],
            'entries'
          ),
        ),
      )
      .addRow(
        $.row('Flush Stats')
        .addPanel(
          $.panel('Queue Length') +
          $.queryPanel('cortex_ingester_flush_queue_length{%s}' % $.jobMatcher($._config.job_names.ingester), '{{pod}}'),
        )
        .addPanel(
          $.panel('Flush Rate') +
          $.qpsPanel('loki_ingester_chunk_age_seconds_count{%s}' % $.jobMatcher($._config.job_names.ingester),),
        ),
      )
      .addRow(
        $.row('Duration')
        .addPanel(
          $.panel('Chunk Duration hours (end-start)') +
          $.queryPanel(
            [
              'histogram_quantile(0.5, sum(rate(loki_ingester_chunk_bounds_hours_bucket{%s}[5m])) by (le))' % $.jobMatcher($._config.job_names.ingester),
              'histogram_quantile(0.99, sum(rate(loki_ingester_chunk_bounds_hours_bucket{%s}[5m])) by (le))' % $.jobMatcher($._config.job_names.ingester),
              'sum(rate(loki_ingester_chunk_bounds_hours_sum{%s}[5m])) / sum(rate(loki_ingester_chunk_bounds_hours_count{%s}[5m]))' % [
                $.jobMatcher($._config.job_names.ingester),
                $.jobMatcher($._config.job_names.ingester),
              ],
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
