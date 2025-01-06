// imports
local config = import '../../config.libsonnet';
local variables = import '../lib/variables.libsonnet';

// helper functions
// handles wrapping an expression in the query_result function as well as stripping new lines, as new lines
// are not supported in variable queries
local query_result(expr) =
  std.strReplace('query_result(%s)' % expr , '\n', ' ');

// regex to extract the value from the query result
local query_regex = '[^\\}]+}\\s+([^ ]+)\\s+.+';

// instead of query being a string, we pass an object as we want the result, but grafonnet doesn't support that that currently
// the object is derived from the schema
local query_result_settings(expr) =
  {
    qryType: 3,
    refId: 'PrometheusVariableQueryEditor-VariableQuery',
    query: expr,
  };

{
  local it = self,
  // if the query is not defaulted, it doesn't auto run for some reason, setting this allows the panels using the variable
  // to render correctly as the variable is not empty.  We also have to set some other values that are not explicitly
  // available through grafonnet
  queryResultOpts(expr, text = '', value = '')::
    {
      definition: expr,
      refresh: 1,
      current: {
        selected: true,
        text: text,
        value: value,
      }
    },

  metrics_datasource: variables.datasource({
      label: 'Metrics Datasource',
      name: 'prom_datasource',
      type: 'prometheus',
      value: config.prom_datasource,
      description: 'The prometheus datasource which contains the Loki metrics',
      regex: '^(?!grafanacloud-(?:ml-metrics|usage)).*',
    }),

  logs_datasource: variables.datasource({
      label: 'Logs Datasource',
      name: 'logs_datasource',
      type: 'loki',
      value: config.logs_datasource,
      description: 'The loki datasource which contains logs that are used for cost attribution',
      regex: '^(?!grafanacloud-.*(?:alert-state-history|usage-insights)).*',
    }),

  cluster:
    local expr = 'label_values(loki_build_info, cluster)';
    variables.query({
      label: 'Cluster',
      name: 'cluster',
      description: 'The cluster of the Loki deployment',
      query: expr,
      datasourceFromVariable: it.metrics_datasource,
      sort: 5,
    })
      + self.queryResultOpts(expr),

  namespace:
    local expr = 'label_values(loki_build_info{cluster="$cluster"}, namespace)';
    variables.query({
      label: 'Namespace',
      name: 'namespace',
      description: 'The namespace of the Loki deployment',
      query: expr,
      datasourceFromVariable: it.metrics_datasource,
      sort: 5,
    })
      + self.queryResultOpts(expr),

  org_id:
    local expr = 'label_values(grafanacloud_org_info, org_id)';
    variables.query({
      name: 'org_id',
      description: 'The id of the organization',
      query: expr,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
    })
      + self.queryResultOpts(expr),

  org_url:
    local expr = 'label_values(grafanacloud_org_info, url)';
    variables.query({
      name: 'org_url',
      description: 'The grafana cloud url for the organization',
      query: expr,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
    })
      + self.queryResultOpts(expr, '12', '12'),

  prepaid_commit_months:
    local expr = query_result(|||
      topk(1,
        ceil(
          (grafanacloud_org_contract_end_date - grafanacloud_org_contract_start_date)
          / (60 * 60 * 24 * 365)
          * 12
        )
        or
        vector(12)
      )
    |||);

    variables.query({
      name: 'prepaid_commit_months',
      description: 'The # of months that the organization has prepaid for, typically in increments of 12.',
      refId: 'PrometheusVariableQueryEditor-VariableQuery',
      query: query_result_settings(expr),
      regex: query_regex,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
    })
      + self.queryResultOpts(expr, '12', '12'),

  group_types:
    // new lines are not supported in variables, even though they are supported in panel queries, so we have to strip them out
    local expr = query_result(|||
      count by (group_type) (
        label_replace(
          grafanacloud_instance_active_usage_group_series,
          "group_type", "$1", "group", "([^:]+):.+"
        )
      )
    |||);

    variables.query({
      name: 'group_types',
      description: 'The available usage groups.',
      query: query_result_settings(expr),
      regex: '\\{group_type="(?<text>[^"]+)".*',
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
      includeAll: true,
      multi: true
    })
      + self.queryResultOpts(expr, ['All'], ['$__all']),

  group_type:
    variables.query({
      name: 'group_types',
      label: 'Group Type',
      description: 'The type of usage group to filter on.',
      query: it.group_types.query,
      regex: it.group_types.regex,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'labelAndValue',
    }),

  usage_groups_by_type:
    // new lines are not supported in variables, even though they are supported in panel queries, so we have to strip them out
    local expr = query_result(|||
      count by (usage_group) (
        label_replace(
          grafanacloud_instance_active_usage_group_series{%s=~"${group_types}:.+"},
          "usage_group", "$1", "group", "[^:]+:(.+)"
        )
      )
    ||| % config.group_label);

    variables.query({
      name: 'usage_groups',
      label: 'Usage Groups',
      description: 'The available usage groups.',
      query: query_result_settings(expr),
      regex: '\\{usage_group="(?<text>[^"]+)".*',
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'labelAndValue',
      includeAll: true,
      customAllValue: '.+',
      multi: true
    })
      + self.queryResultOpts(expr, ['All'], ['$__all']),

  cost_per_series:
    // new lines are not supported in variables, even though they are supported in panel queries, so we have to strip them out
    local expr = query_result(|||
        topk(1,
          max without(monetary) (grafanacloud_org_metrics_overage{org_id="${%s}"})
          /
          grafanacloud_org_metrics_billable_series{org_id="${%s}"}
          or
          vector(0)
        )
      ||| % std.repeat([self.org_id.name], 2)
    );

    variables.query({
      name: 'cost_per_series',
      description: 'The estimated cost per metric series.',
      query: query_result_settings(expr),
      regex: query_regex,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
    })
      + self.queryResultOpts(expr, '0', '0'),

  metric_series_total:
    // new lines are not supported in variables, even though they are supported in panel queries, so we have to strip them out
    local expr = query_result(|||
      topk(1,
        grafanacloud_org_metrics_billable_series{org_id="${%s}"}
        or
        vector(0)
      )
    ||| % self.org_id.name);

    variables.query({
      name: 'metric_series_total',
      description: 'The the total number of metric series for the organization.',
      query: query_result_settings(expr),
      regex: query_regex,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
    })
      + self.queryResultOpts(expr, '0', '0'),

  cost_per_byte:
    // new lines are not supported in variables, even though they are supported in panel queries, so we have to strip them out
    local expr = query_result(|||
      sum(grafanacloud_org_logs_overage{})
      /
      (
        sum(grafanacloud_org_logs_usage{}) * 1e9
      )
      or vector(0)
    |||);

    variables.query({
      name: 'cost_per_byte',
      description: 'The the total number of metric series for the organization.',
      query: query_result_settings(expr),
      regex: query_regex,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
    })
      + self.queryResultOpts(expr, '12', '12'),

  log_bytes_total:
    // new lines are not supported in variables, even though they are supported in panel queries, so we have to strip them out
    local expr = query_result(|||
      sum(grafanacloud_org_logs_usage{}) * 1e9
      or
      vector(0)
    |||);

    variables.query({
      name: 'log_bytes_total',
      description: 'The total number of bytes used by the organization.',
      query: query_result_settings(expr),
      regex: query_regex,
      datasourceFromVariable: it.usage_datasource,
      showOnDashboard: 'nothing',
    })
      + self.queryResultOpts(expr, '0', '0'),
}
