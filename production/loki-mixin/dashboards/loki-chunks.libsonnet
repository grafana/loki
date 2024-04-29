local grafana = import 'grafonnet/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,
    'loki-chunks.json': {
                          local cfg = self,
                          labelsSelector:: $._config.per_cluster_label + '="$cluster", job=~"$namespace/%s"' % (
                            if $._config.meta_monitoring.enabled
                            then '(ingester.*|%s-write|loki-single-binary)' % $._config.ssd.pod_prefix_matcher
                            else if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester.*'
                          ),
                        } +
                        $.dashboard('Loki / Chunks', uid='chunks')
                        .addCluster()
                        .addNamespace()
                        .addTag()
                        .addRow(
                          $.row('Active Series / Chunks')
                          .addPanel(
                            $.newQueryPanel('Series') +
                            $.queryPanel('sum(loki_ingester_memory_chunks{%s})' % dashboards['loki-chunks.json'].labelsSelector, 'series'),
                          )
                          .addPanel(
                            $.newQueryPanel('Chunks per series') +
                            $.queryPanel(
                              'sum(loki_ingester_memory_chunks{%s}) / sum(loki_ingester_memory_streams{%s})' % [
                                dashboards['loki-chunks.json'].labelsSelector,
                                dashboards['loki-chunks.json'].labelsSelector,
                              ],
                              'chunks'
                            ),
                          )
                        )
                        .addRow(
                          $.row('Flush Stats')
                          .addPanel(
                            $.newQueryPanel('Utilization', 'percentunit') +
                            $.latencyPanel('loki_ingester_chunk_utilization', '{%s}' % dashboards['loki-chunks.json'].labelsSelector, multiplier='1'),
                          )
                          .addPanel(
                            $.newQueryPanel('Age') +
                            $.latencyPanel('loki_ingester_chunk_age_seconds', '{%s}' % dashboards['loki-chunks.json'].labelsSelector),
                          ),
                        )
                        .addRow(
                          $.row('Flush Stats')
                          .addPanel(
                            $.newQueryPanel('Log Entries Per Chunk', 'short') +
                            $.latencyPanel('loki_ingester_chunk_entries', '{%s}' % dashboards['loki-chunks.json'].labelsSelector, multiplier='1'),
                          )
                          .addPanel(
                            $.newQueryPanel('Index Entries Per Chunk') +
                            $.queryPanel(
                              'sum(rate(loki_chunk_store_index_entries_per_chunk_sum{%s}[$__rate_interval])) / sum(rate(loki_chunk_store_index_entries_per_chunk_count{%s}[$__rate_interval]))' % [
                                dashboards['loki-chunks.json'].labelsSelector,
                                dashboards['loki-chunks.json'].labelsSelector,
                              ],
                              'Index Entries'
                            ),
                          ),
                        )
                        .addRow(
                          $.row('Flush Stats')
                          .addPanel(
                            $.newQueryPanel('Queue Length') +
                            $.queryPanel('loki_ingester_flush_queue_length{%(label)s} or cortex_ingester_flush_queue_length{%(label)s}' % { label: dashboards['loki-chunks.json'].labelsSelector }, '{{pod}}'),
                          )
                          .addPanel(
                            $.newQueryPanel('Flush Rate') +
                            $.newQpsPanel('loki_ingester_chunk_age_seconds_count{%s}' % dashboards['loki-chunks.json'].labelsSelector,),
                          ),
                        )
                        .addRow(
                          $.row('Flush Stats')
                          .addPanel(
                            $.newQueryPanel('Chunks Flushed/Second') +
                            $.queryPanel('sum(rate(loki_ingester_chunks_flushed_total{%s}[$__rate_interval]))' % dashboards['loki-chunks.json'].labelsSelector, '{{pod}}'),
                          )
                          .addPanel(
                            $.newQueryPanel('Chunk Flush Reason') +
                            $.queryPanel('sum by (reason) (rate(loki_ingester_chunks_flushed_total{%s}[$__rate_interval])) / ignoring(reason) group_left sum(rate(loki_ingester_chunks_flushed_total{%s}[$__rate_interval]))' % [dashboards['loki-chunks.json'].labelsSelector, dashboards['loki-chunks.json'].labelsSelector], '{{reason}}') + {
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
                                  'sum by (le) (rate(loki_ingester_chunk_utilization_bucket{%s}[$__rate_interval]))' % dashboards['loki-chunks.json'].labelsSelector,
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
                                  'sum(rate(loki_ingester_chunk_size_bytes_bucket{%s}[$__rate_interval])) by (le)' % dashboards['loki-chunks.json'].labelsSelector,
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
                                'histogram_quantile(0.99, sum(rate(loki_ingester_chunk_size_bytes_bucket{%s}[$__rate_interval])) by (le))' % dashboards['loki-chunks.json'].labelsSelector,
                                'histogram_quantile(0.90, sum(rate(loki_ingester_chunk_size_bytes_bucket{%s}[$__rate_interval])) by (le))' % dashboards['loki-chunks.json'].labelsSelector,
                                'histogram_quantile(0.50, sum(rate(loki_ingester_chunk_size_bytes_bucket{%s}[$__rate_interval])) by (le))' % dashboards['loki-chunks.json'].labelsSelector,
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
                                'histogram_quantile(0.5, sum(rate(loki_ingester_chunk_bounds_hours_bucket{%s}[$__rate_interval])) by (le))' % dashboards['loki-chunks.json'].labelsSelector,
                                'histogram_quantile(0.99, sum(rate(loki_ingester_chunk_bounds_hours_bucket{%s}[$__rate_interval])) by (le))' % dashboards['loki-chunks.json'].labelsSelector,
                                'sum(rate(loki_ingester_chunk_bounds_hours_sum{%s}[$__rate_interval])) / sum(rate(loki_ingester_chunk_bounds_hours_count{%s}[$__rate_interval]))' % [
                                  dashboards['loki-chunks.json'].labelsSelector,
                                  dashboards['loki-chunks.json'].labelsSelector,
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
