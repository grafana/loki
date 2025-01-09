local g = import '../../lib/grafana.libsonnet';
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';

local promql = g.query.prometheus;
local instantQuery = promql.withInstant(true);
local rangeQuery = promql.withRange(true) + promql.withQueryType('range');

{
  promql:: promql,
  queryType(type)::
    if type == 'instant' then
      instantQuery
    else
      rangeQuery,

}
