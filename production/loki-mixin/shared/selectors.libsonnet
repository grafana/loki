// imports
local config = import '../config.libsonnet';
local variables = import '../dashboards/common/variables.libsonnet';
local utils = import '../lib/utils.libsonnet';

{
  // Creates a new selector object with optional base selectors
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

      // Formats a component selector string based on the component name and current label configuration
      // Returns a regex pattern for pod selectors or a simple string for other resource types
      _formatComponentSelector(component)::
        // convert the component name to camel case
        local componentCamelCase = utils.toCamelCase(component);
        // check if the component exists in the config
        if !std.objectHas(config.components, componentCamelCase) then
          error 'Invalid component: %s' % [component]
        else
          // get the selector value from the config, if it exists, otherwise use the component name
          local selectorValue = if std.objectHas(config.components[componentCamelCase], 'selector_value') then
              config.components[componentCamelCase].selector_value
            else
              config.components[componentCamelCase].component;
            selectorValue,

      // Formats a component selector string based on the component name and current label configuration
      // Returns a regex pattern for pod selectors or a simple string for other resource types
      _formatResourceSelector(component)::
        local selectorValue = self._formatComponentSelector(component);
        local resourceSelector = if !std.objectHas(config.labels, config.labels.resource_selector) then
          error 'Resource selector not found in config.labels: %s' % [config.labels.resource_selector]
        else
          config.labels[config.labels.resource_selector];
        // check if the resource selector is a pod selector
        if std.length(std.findSubstr('pod', resourceSelector)) > 0 then
            '((enterprise|loki)-)?.*%s.*' % selectorValue
        else
          selectorValue,

      // Creates a label selector object with methods for different comparison operations (eq, neq, re, nre)
      label(value):: {
        eq(predicate):: it.selectorLabelEq(value, predicate),
        neq(predicate):: it.selectorLabelNeq(value, predicate),
        re(predicate):: it.selectorLabelRe(value, predicate),
        nre(predicate):: it.selectorLabelNre(value, predicate),
      },

      // Converts various operator formats to their standardized form
      // e.g., 're' -> '=~', 'neq' -> '!=', etc.
      _handleOperator(op)::
        if op == 're' || op == '=~' then '=~'
        else if op == 'nre' || op == '!~' then '!~'
        else if op == 'neq' || op == '!=' then '!='
        else '=',

      // You can also use the label() method directly
      // label('tenant').re('$tenant')

      // Adds cluster label selector using the configured cluster variable
      cluster()::
        self.withLabel(config.labels.cluster, '=', '$' + variables.cluster.name),

      // Adds namespace label selector using the configured namespace variable
      namespace()::
        self.withLabel(config.labels.namespace, '=', '$' + variables.namespace.name),

      // Combines cluster and namespace selectors for common base filtering
      base()::
        self.cluster().namespace(),

      // Creates a selector for a specific Loki component with optional operator
      component(name, op='=~')::
        local formattedValue = self._formatComponentSelector(name);
        self.withLabel(config.labels.component, self._handleOperator(op), formattedValue),

      // Creates a selector matching multiple Loki components using regex
      components(names, op='=~')::
        local formattedValues = std.map(
          function(name)
            self._formatComponentSelector(name),
          names
        );
        self.withLabel(
          config.labels.resource_selector,
          self._handleOperator(op),
          '(' + std.join('|', formattedValues) + ')'
        ),

      // Creates a selector for a specific resource with pod-aware formatting
      resource(value, op='=~')::
        local formattedValue = self._formatResourceSelector(value);
        self.withLabel(config.labels.resource_selector, self._handleOperator(op), formattedValue),

      // Creates a selector matching multiple resources using regex
      resources(values, op='=~')::
        local formattedValues = std.map(
          function(value)
            self._formatResourceSelector(value),
          values
        );
        self.withLabel(
          config.labels.resource_selector,
          self._handleOperator(op),
          '(' + std.join('|', formattedValues) + ')'
        ),

      // Loki components
      adminApi()::
        self.resource('admin-api'),

      bloomBuilder()::
        self.resource('bloom-builder'),

      bloomGateway()::
        self.resource('bloom-gateway'),

      bloomPlanner()::
        self.resource('bloom-planner'),

      compactor()::
        self.resource('compactor'),

      cortexGateway()::
        self.resource('cortex-gw'),

      distributor()::
        self.resource('distributor'),

      gateway()::
        self.resource('gateway'),

      indexGateway()::
        self.resource('index-gateway'),

      ingester()::
        self.resource('ingester'),

      overridesExporter()::
        self.resource('overrides-exporter'),

      patternIngester()::
        self.resource('pattern-ingester'),

      querier()::
        self.resource('querier'),

      queryFrontend()::
        self.resource('query-frontend'),

      queryScheduler()::
        self.resource('query-scheduler'),

      ruler()::
        self.resource('ruler'),

      // Adds tenant label selector using the configured tenant variable
      tenant(op='=')::
        self.withLabel('tenant', self._handleOperator(op), '$' + variables.tenant.name),

      // Adds user label selector using the configured tenant variable
      user(op='=')::
        self.withLabel('user', self._handleOperator(op), '$' + variables.tenant.name),

      // Creates an equality label selector
      selectorLabelEq(label, predicate):: self.withLabel(label, '=', predicate),
      // Creates a not-equals label selector
      selectorLabelNeq(label, predicate):: self.withLabel(label, '!=', predicate),
      // Creates a regex match label selector
      selectorLabelRe(label, predicate):: self.withLabel(label, '=~', predicate),
      // Creates a regex non-match label selector
      selectorLabelNre(label, predicate):: self.withLabel(label, '!~', predicate),

      // Adds multiple label selectors from a key-value object
      withLabels(labels):: self {
        _labels+:: [{ label: k, op: '=', value: labels[k] } for k in std.objectFields(labels)],
      },

      // Adds a single label selector with specified operator and value
      withLabel(label, op='=', value=null):: self {
        local add_label = (
          if std.type(label) == 'object' then
            [label]
          else
            [{ label: label, op: op, value: value }]
        ),
        _labels+:: add_label,
      },

      // Builds the final label selector expression string
      _labelExpr()::
        if self._labels == [] then
          ''
        else
          std.format('%s', std.join(', ', [std.format('%s%s"%s"', [label.label, label.op, label.value]) for label in self._labels])),

      // Generates the complete selector query string
      build()::
        self._labelExpr(),
    };

    // Return either base-initialized or empty selector
    if includeBase then
      selector.cluster().namespace()
    else
      selector,
}
