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
      name: 'metrics_datasource',
      type: 'prometheus',
      value: config.metrics_datasource,
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

  tenant:
    local expr = 'label_values(loki_overrides{cluster="$cluster",namespace="$namespace"}, user)';
    variables.query({
      label: 'Tenant',
      name: 'tenant',
      description: 'The tenant of the Loki deployment',
      query: expr,
      datasourceFromVariable: it.metrics_datasource,
      sort: 5,
    })
      + self.queryResultOpts(expr),
}
