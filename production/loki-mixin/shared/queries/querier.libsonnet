local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';

{
  additional_usage(type = 'instant', legend = 'Additional Usage')::
    common.queryType(type)
      + common.promql.withExpr(|||
          clamp_min(
            max(%s{}) by (id)
            / ignoring(id) group_left()
            max(%s{}) by (id)
            * ignoring(id, org_id, cluster) group_left()
            %s{}
            * on (id) group_left(name)
            %s{},
            0
          )
        ||| % [
          config.traces.metric_names.instance_usage,
          config.traces.metric_names.org_usage,
          config.traces.metric_names.org_overage,
          config.traces.metric_names.instance_info,
        ])
      + common.promql.withRefId('Additional Usage')
      + common.promql.withLegendFormat(legend)
}
