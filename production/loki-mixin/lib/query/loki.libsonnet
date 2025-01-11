// imports
local g = import './grafana.libsonnet';
local utils = import './utils.libsonnet';

// local variables
local query = g.query.loki;
local defaultParams = {
  editorMode:  'code',
  exemplar: null,
  format: null,
  hide: null,
  interval: null,
  intervalFactor: null,
  instant: null,
  legendFormat: null,
  maxLines: null,
  queryType: 'range',
  range: null,
  refId: null,
  step: null,
};

{
  new(datasource, expr, params = {}):: (
    local merged = defaultParams + params;
    local keys = utils.keyNamesFromMethods(query);
    query.new(datasource, expr)
      + utils.applyOptions(query, keys, merged)
  ),
}
