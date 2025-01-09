local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local utils = import '../../../lib/utils.libsonnet';

{
  // base inner query used for each group type
  group_expr_base: |||
      label_replace(
        label_replace(
          sum(
            sum_over_time(%s{%s}[1h:1m])
          ) by (%s),
          "group", "$1", "%s", "(.+)"
        ),
        "group_label", "%s", "", ""
      )
    |||,

  // base inner query used for each group type
  group_lines_expr_base: |||
      label_replace(
        label_replace(
          sum(
            sum_over_time(%s{%s}[1h:1m])
          ) by (%s),
          "group", "$1", "%s", "(.+)"
        ),
        "group_label", "%s", "", ""
      )
    |||,

  groups_expr_base:
    local group_expr_str = std.join(
      '\nor\n',
      [
        self.group_expr_base % ([config.logs.metric_names.log_bytes, ''] + std.repeat([g], 3)),
        for g in config.group_types
      ]
    );

    group_expr_str,

  usage_for_group(type = 'instant')::
    local expr = (
      if type == 'instant' then
        std.strReplace(self.group_expr_base, '[1h:1m]', '[$__range:1m]')
      else
        self.group_expr_base
    );
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(
            expr % ([config.logs.metric_names.log_bytes, ''] + std.repeat(['${%s}' % variables.group_types.name], 3))
          )
      )
      + common.promql.withRefId('GroupUsage'),

  lines_for_group(type = 'instant')::
    local expr = (
      if type == 'instant' then
        std.strReplace(self.group_lines_expr_base, '[1h:1m]', '[$__range:1m]')
      else
        self.group_lines_expr_base
    );
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(
            expr % ([config.logs.metric_names.log_lines, ''] + std.repeat(['${%s}' % variables.group_types.name], 3))
          )
      )
      + common.promql.withRefId('GroupLines'),

  cost_for_group_type_hourly(type = 'instant')::
    local expr = (
      if type == 'instant' then
        std.strReplace(self.group_expr_base, '[1h:1m]', '[$__range:1m]')
      else
        self.group_expr_base
    ) + ' * ${%s}' % variables.cost_per_byte.name;
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(
            expr % ([config.logs.metric_names.log_bytes, ''] + std.repeat(['${%s}' % variables.group_types.name], 3))
          )
      )
      + common.promql.withRefId('GroupCost')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),

  cost_for_usage_group_hourly(type = 'instant')::
    local expr = (
      if type == 'instant' then
        std.strReplace(self.group_expr_base, '[1h:1m]', '[$__range:1m]')
      else
        self.group_expr_base
    ) + ' * ${%s}' % variables.cost_per_byte.name;
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(
            expr % ([config.logs.metric_names.log_bytes, '${%s}=~"${%s}.*2"' % [variables.group_types.name, variables.usage_groups_by_type.name]] + std.repeat(['${%s}' % variables.group_types.name], 3))
          )
      )
      + common.promql.withRefId('GroupCost')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),

  cost_for_usage_group_daily(type = 'instant')::
    local expr = (
      if type == 'instant' then
        std.strReplace(self.group_expr_base, '[1h:1m]', '[$__range:1m]')
      else
        std.strReplace(self.group_expr_base, '[1h:1m]', '[1d:1m]')
    ) + ' * ${%s}' % variables.cost_per_byte.name;
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(
            expr % ([config.logs.metric_names.log_bytes, '${%s}=~"${%s}.*2"' % [variables.group_types.name, variables.usage_groups_by_type.name]] + std.repeat(['${%s}' % variables.group_types.name], 3))
          )
      )
      + common.promql.withRefId('GroupCost')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),

  log_usage(type = 'instant')::
    local expr = (
      if type == 'instant' then
        std.strReplace(self.groups_expr_base, '[1h:1m]', '[$__range:1m]')
      else
        self.groups_expr_base
    );
    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(expr)
        )
      + common.promql.withRefId('GroupUsage'),

  cost_by_group(type = 'instant')::
    // main query used to calculate cost
    local expr = '(%s)' + ('* ${%s}' % variables.cost_per_byte.name);

    common.queryType(type)
      + common.promql.withExpr(
          utils.group_filters(expr % self.groups_expr_base)
        )
      + common.promql.withRefId('GroupCost')
      + common.promql.withLegendFormat('{{%s}}' % config.group_label),

  largest_group_spend:
    local qry = self.cost_by_group('instant');
    common.queryType('instant')
      + common.promql.withExpr('topk(1, %s)' % qry.expr)
      + common.promql.withRefId('LargestSpend'),
}
