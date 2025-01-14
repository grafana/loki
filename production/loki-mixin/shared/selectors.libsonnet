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

      // Formats a component/resource selector string based on the component name and current label configuration
      // Returns a regex pattern for pod selectors or a simple string for other resource types
      _formatResourceSelector(component, type=config.labels.resource_selector)::
        // convert the component name to camel case
        local componentCamelCase = utils.toCamelCase(component);
        local selectorValue = (
          // check if the component exists in the config
          if !std.objectHas(config.components, componentCamelCase) then
            error 'Invalid component: %s, the component does not exist in config.components' % [component]
          // check if the component has a selector_value, if it does, use that, otherwise use the component name
          else if std.objectHas(config.components[componentCamelCase], 'selector_value') then
            config.components[componentCamelCase].selector_value
          else
            config.components[componentCamelCase].component
        );
        local resourceSelector = (
          if !std.objectHas(config.labels, type) then
            error 'Invalid resource selector: %s, no label found in config.labels' % [type]
          else
            config.labels[type]
        );
        // check if the resource selector is a pod selector
        if type == 'pod' then
            '((enterprise|loki)-)?.*%s.*' % selectorValue
        else
          selectorValue,

      // Creates a selector for a specific Loki component with optional operator
      component(name, op='=~')::
        local formattedValue = self._formatResourceSelector(name, type='component');
        self.withLabel(config.labels.component, self._handleOperator(op), formattedValue),

      // Creates a selector matching multiple Loki components using regex
      components(names, op='=~')::
        local formattedValues = std.map(
          function(name)
            self._formatResourceSelector(name, type='component'),
          names
        );
        self.withLabel(
          config.labels.component,
          self._handleOperator(op),
          '(' + std.join('|', formattedValues) + ')'
        ),

      // Creates a selector for a specific Loki pod with optional operator
      pod(name, op='=~')::
        local formattedValue = self._formatResourceSelector(name, type='pod');
        local baseSelector = self.withLabel(config.labels.pod, self._handleOperator(op), formattedValue);
        if op == '=~' || op == 're' then
          std.foldl(
            function(acc, negation) acc.withLabel(negation.label, negation.op, negation.value),
            self._generatePodNegations(name),
            baseSelector
          )
        else
          baseSelector,

      // Creates a selector matching multiple Loki pods using regex
      pods(names, op='=~')::
        local formattedValues = std.map(
          function(name)
            self._formatResourceSelector(name, type='pod'),
          names
        );
        local baseSelector = self.withLabel(
          config.labels.pod,
          self._handleOperator(op),
          '(' + std.join('|', formattedValues) + ')'
        );
        if op == '=~' || op == 're' then
          std.foldl(
            function(acc, name)
              std.foldl(
                function(innerAcc, negation) innerAcc.withLabel(negation.label, negation.op, negation.value),
                self._generatePodNegations(name),
                acc
              ),
            names,
            baseSelector
          )
        else
          baseSelector,

      // Creates a selector for a specific Loki container with optional operator
      container(name, op='=~')::
        local formattedValue = self._formatResourceSelector(name, type='container');
        self.withLabel(config.labels.container, self._handleOperator(op), formattedValue),

      // Creates a selector matching multiple Loki containers using regex
      containers(names, op='=~')::
        local formattedValues = std.map(
          function(name)
            self._formatResourceSelector(name, type='container'),
          names
        );
        self.withLabel(
          config.labels.container,
          self._handleOperator(op),
          '(' + std.join('|', formattedValues) + ')'
        ),

      // Creates a selector for a specific resource dynamically based on the config value (component/pod/container) with pod-aware formatting
      resource(value, op='=~', type=config.labels.resource_selector)::
        local formattedValue = self._formatResourceSelector(value, type=type);
        local baseSelector = self.withLabel(config.labels.resource_selector, self._handleOperator(op), formattedValue);
        if type == 'pod' && (op == '=~' || op == 're') then
          std.foldl(
            function(acc, negation) acc.withLabel(negation.label, negation.op, negation.value),
            self._generatePodNegations(value),
            baseSelector
          )
        else
          baseSelector,

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

      // Adds user label selector using the configured tenant variable
      job(op='=')::
        if config.selectors.job != '' then
          self.withLabel(config.labels.job, self._handleOperator(op), config.selectors.job)
        else
          self,

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

      // Helper function to generate negation patterns for pod selectors
      _generatePodNegations(component)::
        local componentCamelCase = utils.toCamelCase(component);
        local currentComponent = config.components[componentCamelCase].component;
        local negations = std.filter(
          function(c)
            // Only include components where:
            // 1. It's not the current component
            // 2. Its component name contains the current component name
            c != componentCamelCase &&
            std.length(std.findSubstr(currentComponent, config.components[c].component)) > 0,
          std.objectFields(config.components)
        );
        [
          {
            label: config.labels.pod,
            op: '!~',
            value: '((enterprise|loki)-)?.*%s.*' % config.components[c].component,
          }
          for c in negations
        ],
    };

    // Return either base-initialized or empty selector
    if includeBase then
      selector.cluster().namespace()
    else
      selector,
}
