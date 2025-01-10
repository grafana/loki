// imports
local config = import '../config.libsonnet';
local variables = import '../dashboards/common/variables.libsonnet';

{
  new(includeBase=true)::
    local selector = {
      local it = self,
      _labels:: [],

      // Component patterns for different deployment modes
      // TODO: this is not currently used, need to determine if we need to support SSD still
      _componentPatterns:: {
        ingester: '(ingester|((enterprise|loki)-)?write)',
        distributor: '(distributor|((enterprise|loki)-)?write)',
        write: '(ingester|distributor|((enterprise|loki)-)?write)',
        backend: '(backend|((enterprise|loki)-)?backend)',
        querier: '(querier|((enterprise|loki)-)?read)',
        'query-scheduler': '(query-scheduler|((enterprise|loki)-)?read)',
        'query-frontend': '(query-frontend|((enterprise|loki)-)?read)',
        'overrides-exporter': '(overrides-exporter|((enterprise|loki)-)?backend)',
      },

      // Helper to format component selector based on label type
      _formatComponentSelector(component)::
        if std.length(std.findSubstr('pod', config.labels.resource_selector)) > 0
        then '((enterprise|loki)-)?.*%s.*' % component
        else component,

      // Enhanced selector methods
      label(value):: {
        eq(predicate):: it.selectorLabelEq(value, predicate),
        neq(predicate):: it.selectorLabelNeq(value, predicate),
        re(predicate):: it.selectorLabelRe(value, predicate),
        nre(predicate):: it.selectorLabelNre(value, predicate),
      },

      // Helper to handle operator selection
      _handleOperator(op)::
        if op == 're' || op == '=~' then '=~'
        else if op == 'nre' || op == '!~' then '!~'
        else if op == 'neq' || op == '!=' then '!='
        else '=',

      // You can also use the label() method directly
      // label('tenant').re('$tenant')

      // Common selectors
      cluster()::
        self.withLabel(config.labels.cluster, '=', '$' + variables.cluster.name),

      namespace()::
        self.withLabel(config.labels.namespace, '=', '$' + variables.namespace.name),

      base()::
        self.cluster().namespace(),

      // Component selector specifically for Loki components
      component(name, op='=')::
        self.withLabel(config.labels.resource_selector, self._handleOperator(op), self._formatComponentSelector(name)),

      adminApi(op='=~')::
        self.component('admin-api', op),

      bloomGateway(op='=~')::
        self.component('bloom-gateway', op),

      bloomBuilder(op='=~')::
        self.component('bloom-builder', op),

      bloomPlanner(op='=~')::
        self.component('bloom-planner', op),

      gateway(op='=~')::
        self.component('gateway', op),

      ingester(op='=~')::
        self.component('ingester', op),

      indexGateway(op='=~')::
        self.component('index-gateway', op),

      distributor(op='=~')::
        self.component('distributor', op),

      write(op='=~')::
        self.component('write', op),

      querier(op='=~')::
        self.component('querier', op),

      queryScheduler(op='=~')::
        self.component('query-scheduler', op),

      queryFrontend(op='=~')::
        self.component('query-frontend', op),

      ruler(op='=~')::
        self.component('ruler', op),

      overridesExporter(op='=~')::
        self.component('overrides-exporter', op),

      tenant(op='=')::
        self.withLabel('tenant', self._handleOperator(op), '$' + variables.tenant.name),

      user(op='=')::
        self.withLabel('user', self._handleOperator(op), '$' + variables.tenant.name),

      // Base methods remain the same
      selectorLabelEq(label, predicate):: self.withLabel(label, '=', predicate),
      selectorLabelNeq(label, predicate):: self.withLabel(label, '!=', predicate),
      selectorLabelRe(label, predicate):: self.withLabel(label, '=~', predicate),
      selectorLabelNre(label, predicate):: self.withLabel(label, '!~', predicate),

      withLabels(labels):: self {
        _labels+:: [{ label: k, op: '=', value: labels[k] } for k in std.objectFields(labels)],
      },

      withLabel(label, op='=', value=null):: self {
        local add_label = (
          if std.type(label) == 'object' then
            [label]
          else
            [{ label: label, op: op, value: value }]
        ),
        _labels+:: add_label,
      },

      _labelExpr()::
        if self._labels == [] then
          ''
        else
          std.format('%s', std.join(', ', [std.format('%s%s"%s"', [label.label, label.op, label.value]) for label in self._labels])),

      // builds the query
      build()::
        self._labelExpr(),
    };

    // Return either base-initialized or empty selector
    if includeBase then
      selector.cluster().namespace()
    else
      selector,
}
