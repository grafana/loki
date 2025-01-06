(import './panels/helpers.libsonnet') {
  barChart: (import './panels/bar-chart/base.libsonnet'),
  gauge: (import './panels/gauge/base.libsonnet'),
  row: (import './panels/row.libsonnet'),
  stat: import './panels/stat/base.libsonnet',
  table: (import './panels/table/base.libsonnet'),
  text: (import './panels/text/base.libsonnet'),
  timeSeries: (import './panels/time-series/base.libsonnet'),
  treemap: (import './panels/treemap/base.libsonnet'),
}
