local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local utils = import '../../../lib/utils.libsonnet';

{
  active_series(type = 'instant', legend = 'Active')::
    common.queryType(type)
      + common.promql.withExpr(|||
        max(%s{}) by (id)
        * on (id) group_left(name)
        %s{}
      ||| % [
        config.metrics.metric_names.instance_active_series,
        config.overview.metric_names.instance_info,
      ])
      + common.promql.withRefId('Active')
      + common.promql.withLegendFormat('Active Series'),

  cost_by_group(type = 'instant')::
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(|||
            quantile_over_time(
              0.95,
              max by(%s) (
                (%s{org_id="${org_id}"} < Inf)
                or
                absent(%s{org_id="${org_id}"}) * 0
              )[$__range:]
            )
            * on() group_left()
            # Price per billable-series:
            (
              max without(monetary) (%s{org_id="${org_id}"})
              /
              %s{org_id="${org_id}"}
            )
          ||| % [
            config.group_label,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.metrics.metric_names.org_overage,
            config.metrics.metric_names.org_usage,
          ])
        )
      + common.promql.withRefId('GroupCost')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),

  largest_group_spend:
    local qry = self.cost_by_group('instant');
    common.queryType('instant')
      + common.promql.withExpr('topk(1, %s)' % qry.expr)
      + common.promql.withRefId('LargestSpend'),

  usage_by_group(type = 'instant')::
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(|||
            label_replace(
              label_replace(
                quantile_over_time(
                  0.95,
                  max by(%s) (
                    (
                      %s{org_id="${org_id}"} < Inf
                    )
                    or
                    absent(
                      %s{org_id="${org_id}"}
                    ) * 0
                  )[$__range:]
                ),
                "group_label", "$1", "%s", "([^:]+):.*"
              ),
              "group", "$1", "%s", "[^:]+:(.*)"
            )
          ||| % [
            config.group_label,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.group_label,
            config.group_label,
          ])
        )
      + common.promql.withRefId('GroupUsage')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),

  cost_for_group_type_hourly(type = 'instant')::
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(|||
            label_replace(
              label_replace(
                quantile_over_time(
                    0.95,
                    max by(%s) (
                        (%s{%s=~"${group_types}:${usage_groups}"} < Inf)
                        or
                        absent(%s{org_id="${org_id}",%s=~"${group_types}:${usage_groups}"})*0
                    )[$__range:]
                )
                * on() group_left()
                # Price per billable-series:
                (
                  max without(monetary) (
                    %s{org_id="${org_id}"})
                    /
                    %s{org_id="${org_id}"}
                  ),
                "group_label", "$1", "%s", "([^:]+):.*"
              ),
              "group", "$1", "%s", "[^:]+:(.*)"
            )
          ||| % [
            config.group_label,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.group_label,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.group_label,
            config.metrics.metric_names.org_overage,
            config.metrics.metric_names.org_usage,
            config.group_label,
            config.group_label,
          ]
          )
        )
      + common.promql.withRefId('GroupCost')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),

  usage_for_group(type = 'instant')::
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(|||
            label_replace(
              label_replace(
                quantile_over_time(
                    0.95,
                    max by(%s) (
                        (%s{org_id="${org_id}",%s=~"${group_types}:.+"} < Inf)
                        or
                        absent(%s{org_id="${org_id}",%s=~"${group_types}:.+"}) * 0
                    )[$__range:]
                ),
                "group_label", "$1", "%s", "([^:]+):.*"
              ),
              "group", "$1", "%s", "[^:]+:(.*)"
            )
          ||| % [
            config.group_label,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.group_label,
            config.metrics.metric_names.instance_active_usage_group_series,
            config.group_label,
            config.group_label,
            config.group_label,
          ])
        )
      + common.promql.withRefId('GroupUsage')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),
}
