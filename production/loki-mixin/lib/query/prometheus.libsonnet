// imports
local g = import './grafana.libsonnet';
local utils = import './utils.libsonnet';

// local variables
local query = g.query.prometheus;
local defaultParams = {
  editorMode:  'code',
  exemplar: null,
  format: 'time_series',
  hide: null,
  interval: null,
  intervalFactor: null,
  instant: null,
  legendFormat: null,
  queryType: 'range',
  range: null,
  refId: null,
};

{
  new(datasource, expr, params = {}):: (
    local merged = defaultParams + params;
    local keys = utils.keyNamesFromMethods(query);
    query.new(datasource, expr)
      + utils.applyOptions(query, keys, merged)
  ),
}
