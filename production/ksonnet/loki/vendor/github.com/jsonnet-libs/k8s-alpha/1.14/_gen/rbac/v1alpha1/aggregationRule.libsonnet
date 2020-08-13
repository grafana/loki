{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='aggregationRule', url='', help='AggregationRule describes how to locate ClusterRoles to aggregate into the ClusterRole'),
  '#withClusterRoleSelectors':: d.fn(help="ClusterRoleSelectors holds a list of selectors which will be used to find ClusterRoles and create the rules. If any of the selectors match, then the ClusterRole's permissions will be added", args=[d.arg(name='clusterRoleSelectors', type=d.T.array)]),
  withClusterRoleSelectors(clusterRoleSelectors): { clusterRoleSelectors: if std.isArray(v=clusterRoleSelectors) then clusterRoleSelectors else [clusterRoleSelectors] },
  '#withClusterRoleSelectorsMixin':: d.fn(help="ClusterRoleSelectors holds a list of selectors which will be used to find ClusterRoles and create the rules. If any of the selectors match, then the ClusterRole's permissions will be added\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='clusterRoleSelectors', type=d.T.array)]),
  withClusterRoleSelectorsMixin(clusterRoleSelectors): { clusterRoleSelectors+: if std.isArray(v=clusterRoleSelectors) then clusterRoleSelectors else [clusterRoleSelectors] },
  '#mixin': 'ignore',
  mixin: self
}