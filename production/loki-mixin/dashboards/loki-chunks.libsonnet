local grafana = import 'grafonnet/grafana.libsonnet';
local selector = (import '../selectors.libsonnet').new;

local selectors = {
  ingester: selector().job(['ingester', 'partition-ingester']),
};

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    'loki-chunks.json':
      $.dashboard('Loki / Chunks', uid='chunks')
      .addCluster()
      .addNamespace()
      .addTag()
      .addRow(
        $.row('Active Series / Chunks')
        .addPanel(
          $.newQueryPanel('Series') +
          $.queryPanel('sum(loki_ingester_memory_chunks{%s})' % selectors.ingester.build(), 'series'),
        )
        .addPanel(
          $.newQueryPanel('Chunks per series') +
          $.queryPanel(
            |||
              sum(loki_ingester_memory_chunks{%(selector)s})
              /
              sum(loki_ingester_memory_streams{%(selector)s})
            ||| % { selector: selectors.ingester.build() },
            'chunks'
          ),
        )
      )
      .addRow(
        $.row('Flush Stats')
        .addPanel(
          $.newQueryPanel('Utilization', 'percentunit') +
          $.latencyPanel('loki_ingester_chunk_utilization', selectors.ingester.build(brackets=true), multiplier='1'),
        )
        .addPanel(
          $.newQueryPanel('Age') +
          $.latencyPanel('loki_ingester_chunk_age_seconds', selectors.ingester.build(brackets=true)),
        ),
      )
      .addRow(
        $.row('Flush Stats')
        .addPanel(
          $.newQueryPanel('Log Entries Per Chunk', 'short') +
          $.latencyPanel('loki_ingester_chunk_entries', selectors.ingester.build(brackets=true), multiplier='1'),
        )
        .addPanel(
          $.newQueryPanel('Index Entries Per Chunk') +
          $.queryPanel(
            |||
              sum(rate(loki_chunk_store_index_entries_per_chunk_sum{%(selector)s}[$__rate_interval]))
              /
              sum(rate(loki_chunk_store_index_entries_per_chunk_count{%(selector)s}[$__rate_interval]))
            ||| % { selector: selectors.ingester.build() },
            'Index Entries'
          ),
        ),
      )
      .addRow(
        $.row('Flush Stats')
        .addPanel(
          $.newQueryPanel('Queue Length') +
          $.queryPanel(
            |||
              loki_ingester_flush_queue_length{%(selector)s}
              or
              cortex_ingester_flush_queue_length{%(selector)s}
            ||| % { selector: selectors.ingester.build() },
            '{{pod}}',
          ),
        )
        .addPanel(
          $.newQueryPanel('Flush Rate') +
          $.newQpsPanel('loki_ingester_chunk_age_seconds_count{%s}' % selectors.ingester.build()),
        ),
      )
      .addRow(
        $.row('Flush Stats')
        .addPanel(
          $.newQueryPanel('Chunks Flushed/Second') +
          $.queryPanel(
            |||
              sum(
                rate(loki_ingester_chunks_flushed_total{%(selector)s}[$__rate_interval])
              )
            ||| % { selector: selectors.ingester.build() },
            '{{pod}}',
          ),
        )
        .addPanel(
          $.newQueryPanel('Chunk Flush Reason') +
          $.queryPanel(
            |||
              sum by (reason) (rate(loki_ingester_chunks_flushed_total{%(selector)s}[$__rate_interval]))
              / ignoring(reason) group_left
              sum(rate(loki_ingester_chunks_flushed_total{%(selector)s}[$__rate_interval]))
            ||| % { selector: selectors.ingester.build() },
            '{{reason}}',
          ) + {
            stack: true,
            yaxes: [
              { format: 'short', label: null, logBase: 1, max: 1, min: 0, show: true },
              { format: 'short', label: null, logBase: 1, max: 1, min: null, show: false },
            ],
          },
        ),
      )
      .addRow(
        $.row('Utilization')
        .addPanel(
          grafana.heatmapPanel.new(
            'Chunk Utilization',
            datasource='$datasource',
            yAxis_format='percentunit',
            tooltip_showHistogram=true,
            color_colorScheme='interpolateSpectral',
            dataFormat='tsbuckets',
            yAxis_decimals=0,
            legend_show=true,
          ).addTargets(
            [
              grafana.prometheus.target(
                |||
                  sum by (le) (
                    rate(loki_ingester_chunk_utilization_bucket{%(selector)s}[$__rate_interval])
                  )
                ||| % { selector: selectors.ingester.build() },
                legendFormat='{{le}}',
                format='heatmap',
              ),
            ],
          )
        )
      )
      .addRow(
        $.row('Utilization')
        .addPanel(
          grafana.heatmapPanel.new(
            'Chunk Size Bytes',
            datasource='$datasource',
            yAxis_format='bytes',
            tooltip_showHistogram=true,
            color_colorScheme='interpolateSpectral',
            dataFormat='tsbuckets',
            yAxis_decimals=0,
            // tooltipDecimals=3,
            // span=3,
            legend_show=true,
          ).addTargets(
            [
              grafana.prometheus.target(
                |||
                  sum by (le)(
                    rate(loki_ingester_chunk_size_bytes_bucket{%(selector)s}[$__rate_interval])
                  )
                ||| % { selector: selectors.ingester.build() },
                legendFormat='{{le}}',
                format='heatmap',
              ),
            ],
          )
        )
      )
      .addRow(
        $.row('Utilization')
        .addPanel(
          $.newQueryPanel('Chunk Size Quantiles', 'bytes') +
          $.queryPanel(
            [
              |||
                histogram_quantile(
                  0.99,
                  sum by (le)(
                    rate(loki_ingester_chunk_size_bytes_bucket{%(selector)s}[$__rate_interval])
                  )
                )
              ||| % { selector: selectors.ingester.build() },
              |||
                histogram_quantile(
                  0.90,
                  sum by (le)(
                    rate(loki_ingester_chunk_size_bytes_bucket{%(selector)s}[$__rate_interval])
                  )
                )
              ||| % { selector: selectors.ingester.build() },
              |||
                histogram_quantile(
                  0.50,
                  sum by (le)(
                    rate(loki_ingester_chunk_size_bytes_bucket{%(selector)s}[$__rate_interval])
                  )
                )
              ||| % { selector: selectors.ingester.build() },
            ],
            [
              'p99',
              'p90',
              'p50',
            ],
          ),
        )
      )
      .addRow(
        $.row('Duration')
        .addPanel(
          $.newQueryPanel('Chunk Duration hours (end-start)') +
          $.queryPanel(
            [
              |||
                histogram_quantile(
                  0.5,
                  sum by (le)(
                    rate(loki_ingester_chunk_bounds_hours_bucket{%(selector)s}[$__rate_interval])
                  )
                )
              ||| % { selector: selectors.ingester.build() },
              |||
                histogram_quantile(
                  0.99,
                  sum by (le)(
                    rate(loki_ingester_chunk_bounds_hours_bucket{%(selector)s}[$__rate_interval])
                  )
                )
              ||| % { selector: selectors.ingester.build() },
              |||
                sum(
                  rate(loki_ingester_chunk_bounds_hours_sum{%(selector)s}[$__rate_interval])
                )
                /
                sum(
                  rate(loki_ingester_chunk_bounds_hours_count{%(selector)s}[$__rate_interval])
                )
              ||| % { selector: selectors.ingester.build() },
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
