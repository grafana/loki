local g = import '../../lib/grafana.libsonnet';
local config = import '../../../config.libsonnet';
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

  usage(service, type = 'instant', legend = 'Billable Usage')::
    self.queryType(type)
      + promql.withExpr('%s{}' % config[service].metric_names.org_usage)
      + promql.withRefId('Usage')
      + promql.withLegendFormat(legend),

  usage_utilization(service, type = 'instant', legend = 'Usage Utilization')::
    self.queryType(type)
      + promql.withExpr(|||
          %s{}
          /
          %s{}
        ||| % [
          config[service].metric_names.org_usage,
          config[service].metric_names.org_included_usage,
        ])
      + promql.withRefId('Usage')
      + promql.withLegendFormat(legend),

  additional_usage(service, type = 'instant', legend = 'Additional Usage')::
    self.queryType(type)
      + promql.withExpr(|||
          clamp_min(
            %s{}
            -
            %s{},
            0
          )
        ||| % [
          config[service].metric_names.org_usage,
          config[service].metric_names.org_included_usage,
        ])
      + promql.withRefId('Additional')
      + promql.withLegendFormat(legend),

  included_usage(service, type = 'instant', legend = 'Included Usage')::
    self.queryType(type)
      + (
        if service == 'metrics' then
          promql.withExpr('grafanacloud_org_metrics_included_series{}')
        else
          promql.withExpr('grafanacloud_org_%s_included_usage{}' % service)
      )
      + promql.withRefId('Included')
      + promql.withLegendFormat(legend),

  spend(service, type = 'instant', legend = 'Spend')::
    self.queryType(type)
      + promql.withExpr('max without(monetary) (grafanacloud_org_%s_overage{})' % service)
      + promql.withRefId('Spend')
      + promql.withLegendFormat(legend),

  total_spend(service, type = 'range', legend = 'Burn')::
    self.queryType(type)
      + promql.withExpr(|||
        sum(grafanacloud_org_%s_overage{})
        and
        day_of_month() == days_in_month()
        and
        hour() == 23
      ||| % service)
      + promql.withRefId(service)
      + promql.withLegendFormat(legend),
}
