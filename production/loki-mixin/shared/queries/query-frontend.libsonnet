local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';

{
  spend(type = 'instant', legend = 'Spend')::
    common.queryType(type)
      // there was a new label introduced in Oct-2023 called monetary="true", that can result in multiple series when only
      // one is needed, so we use topk(1) to get a single result
      + common.promql.withExpr(|||
        max without (monetary) (%s{})
      ||| % [
        config.overview.metric_names.org_total_overage,
      ])
      + common.promql.withRefId('Usage')
    + common.promql.withLegendFormat(legend),

  spend_utilization:
    common.queryType('instant')
      + common.promql.withExpr(|||
        max(%s{})
        /
        max(
          %s{}
          /
          ${%s}
        )
      ||| % [
        config.overview.metric_names.org_total_overage,
        config.overview.metric_names.org_spend_commit_credit_total,
        variables.prepaid_commit_months.name,
      ])
      + common.promql.withRefId('Spend Utilization'),

  current_balance:
    common.queryType('instant')
      + common.promql.withExpr(|||
        max(%s{})
      ||| % [
        config.overview.metric_names.org_spend_commit_balance_total,
      ])
      + common.promql.withRefId('Balance'),

  forecast_balance:
    common.queryType('instant')
      + common.promql.withExpr(|||
        max(%s{})
        -
        max(%s{})
      ||| % [
        config.overview.metric_names.org_spend_commit_balance_total,
        config.overview.metric_names.org_total_overage,
      ])
      + common.promql.withRefId('Forecast Balance'),

  budget:
    common.queryType('range')
      + common.promql.withExpr(|||
        avg without (monetary) (
          %s{}
          /
          ${%s}
        )
      ||| % [
        config.overview.metric_names.org_spend_commit_balance_total,
        variables.prepaid_commit_months.name
      ])
      + common.promql.withRefId('Spend Utilization')
      + common.promql.withLegendFormat('Monthly Budget'),

  credit_balance:
    common.queryType('range')
      + common.promql.withExpr(|||
        sum(%s{})
        -
        sum({__name__=~"grafanacloud_org_.+_overage"})
        and
        day_of_month() == days_in_month()
        and hour() == 23
      ||| % [
        config.overview.metric_names.org_spend_commit_balance_total,
      ])
      + common.promql.withRefId('Credit Balance')
      + common.promql.withLegendFormat('Credit Balance'),

  total_spend:
    common.queryType('range')
      + common.promql.withExpr(|||
        sum({__name__=~"grafanacloud_org_.+_overage"})
        and
        day_of_month() == days_in_month()
        and
        hour() == 23
      |||)
      + common.promql.withRefId('Spend')
      + common.promql.withLegendFormat('Spend'),
}
