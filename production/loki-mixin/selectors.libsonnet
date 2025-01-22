(import 'config.libsonnet') {
  local cfg = self,
  // Creates a new selector object with optional base selectors
  new(includeBase=true)::
    local selector = {
      local root = self,
      _labels:: [],

      // Creates a label selector object with methods for different comparison operations (eq, neq, re, nre)
      // Example: selector().label('tenant').eq('$tenant')
      label(value):: {
        eq(predicate):: root.selectorLabelEq(value, predicate),
        neq(predicate):: root.selectorLabelNeq(value, predicate),
        re(predicate):: root.selectorLabelRe(value, predicate),
        nre(predicate):: root.selectorLabelNre(value, predicate),
      },

      // Creates an equality label selector
      selectorLabelEq(label, predicate):: self.withLabel(label, '=', predicate),
      // Creates a not-equals label selector
      selectorLabelNeq(label, predicate):: self.withLabel(label, '!=', predicate),
      // Creates a regex match label selector
      selectorLabelRe(label, predicate):: self.withLabel(label, '=~', predicate),
      // Creates a regex non-match label selector
      selectorLabelNre(label, predicate):: self.withLabel(label, '!~', predicate),

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
      // shorthand for selector().label('cluster').eq('$cluster')
      cluster()::
        self.withLabel(cfg._config.labels.cluster, '=', '$cluster'),

      // Adds namespace label selector using the configured namespace variable
      // shorthand for selector().label('namespace').eq('$namespace')
      namespace()::
        self.withLabel(cfg._config.labels.namespace, '=', '$namespace'),

      // Adds tenant label selector using the configured tenant variable
      // shorthand for selector().label('tenant').eq('$tenant')
      tenant(value='$tenant', op='=')::
        self.withLabel('tenant', op, value),

      // Adds user label selector using the configured tenant variable
      // shorthand for selector().label('user').eq('$user')
      user(value='$user', op='=')::
        self.withLabel('user', op, value),

      // Adds route label selector using the configured route variable
      // shorthand for selector().label('route').eq('$route')
      route(value='$route', op='=~')::
        self.withLabel('route', op, value),

      // Combines cluster and namespace selectors for common base filtering
      base()::
        self.cluster().namespace(),

      // gets a key from a component in the config, if the component or key doesn't exist in the config, return the default value
      _getComponentKey(component, key, default=null)::
         // check if the component exists in the config
        if !std.objectHas(cfg._config.components, component) then
          error 'Invalid component: %s, the component does not exist in config.components' % [component]
        // check if the key exists in the component
        else if std.objectHas(cfg._config.components[component], key) then
          cfg._config.components[component][key]
        else
          default,

      // Formats the base matcher for a job/pod/container/component based on the component name and config settings
      // accounts for ssd and loki-single-binary
      // if the component is an array, call the function again for each element and join with a pipe
      _formatBaseMatcher(components, label)::
        // loop over the list of existing matcher labels that have already been added, if the label matchers have a key of the passed
        // label, save it and add it to the list of passed components.  this handles the case where multiple shorthand components
        // are used i.e. selector().querier().queryFrontend().queryScheduler() which would render the same result as
        // selector().job(['querier', 'query-frontend', 'query-scheduler'])
        local mergedComponents = (
          if std.isArray(components) then components else [components]
          +
          // get the list of existing labels/matchers that have the passed label i.e. job, pod, container, component
          std.map(
            function(matcher) matcher[label],
            std.filter(
              function(matcher)
                std.objectHas(matcher, label),
              root._labels
            )
          )
        );
        // an array of each of the matchers, which could contain duplicates,
        local formattedMatchers = std.map(
          function(c)
            local componentPattern = self._getComponentKey(component=c, key='pattern', default=c);
            local componentPath = self._getComponentKey(component=c, key='ssd_path');
            std.join('|', [componentPattern, componentPath, 'single-binary']),
          mergedComponents
        );
        // we need to convert the array of matchers to a single string, with no duplicates
        std.join('|',
          std.uniq(
            std.sort(
              std.split(std.join('|', formattedMatchers), '|')
            )
          )
        ),

      // Formats the matcher for a job/pod/container/component based on the component name and config settings
      _formatMatcher(matcher, pattern='%(prefix)s(%(matcher)s)')::
        std.format(pattern, {
          prefix: (
            if cfg._config.meta_monitoring.enabled then
              cfg._config.meta_monitoring.job_prefix
            else
              ''
          ),
          matcher: matcher,
        }),

      // Creates a job matcher for a specific Loki component with optional operator
      job(jobs, op='=~')::
        local formattedMatchers = self._formatBaseMatcher(components=jobs, label='job');
        local formattedSelector = self._formatMatcher(
          matcher=formattedMatchers,
          pattern='($namespace)/%(prefix)s(%(matcher)s)',
        );
        self.withLabel(
          label=cfg._config.labels.job,
          op=self._handleOperator(op),
          value=formattedSelector,
          metadata={
            job: jobs,
          },
        ),

      // Creates a matcher for a specific Loki pod with optional operator
      pod(pods, op='=~')::
        local formattedMatchers = self._formatBaseMatcher(components=pods, label='pod');
        local formattedSelector = self._formatMatcher(
          matcher=formattedMatchers,
          // match the statefulset or deployment name and the pod name
          pattern='%(prefix)s((%(matcher)s)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))',
        );
        self.withLabel(
          label=cfg._config.labels.pod,
          op=self._handleOperator(op),
          value=formattedSelector,
          metadata={
            pod: pods,
          },
        ),

      // Creates a container matcher for a specific Loki component with optional operator
      container(containers, op='=~')::
        local formattedMatchers = self._formatBaseMatcher(components=containers, label='container');
        local formattedSelector = self._formatMatcher(
          matcher=formattedMatchers,
          pattern='%(prefix)s(%(matcher)s)',
        );
        self.withLabel(
          label=cfg._config.labels.container,
          op=self._handleOperator(op),
          value=formattedSelector,
          metadata={
            container: containers,
          },
        ),

      // Creates a component matcher for a specific Loki component with optional operator
      component(components, op='=~')::
        local formattedMatchers = self._formatBaseMatcher(components=components, label='component');
        local formattedSelector = self._formatMatcher(
          matcher=formattedMatchers,
          pattern='%(prefix)s(%(matcher)s)',
        );
        self.withLabel(
          label=cfg._config.labels.job,
          op=self._handleOperator(op),
          value=formattedSelector,
          metadata={
            component: components,
          },
        ),

      // wrapper for calling the correct matcher method based on the label
      resource(label, value, op='=~')::
        local camelCaseLabel = cfg.toCamelCase(label);
        if std.objectHasAll(root, camelCaseLabel) then
          root[camelCaseLabel](value, op)
        else
          error 'Invalid resource: %s, no selector method found for this resource' % [label],

      // Loki components
      adminApi(label='job', op='=~')::
        self.resource(label=label, value='admin-api', op=op),

      bloomBuilder(label='job', op='=~')::
        self.resource(label=label, value='bloom-builder', op=op),

      bloomGateway(label='job', op='=~')::
        self.resource(label=label, value='bloom-gateway', op=op),

      bloomPlanner(label='job', op='=~')::
        self.resource(label=label, value='bloom-planner', op=op),

      compactor(label='job', op='=~')::
        self.resource(label=label, value='compactor', op=op),

      cortexGateway(label='job', op='=~')::
        self.resource(label=label, value='cortex-gw', op=op),

      distributor(label='job', op='=~')::
        self.resource(label=label, value='distributor', op=op),

      gateway(label='job', op='=~')::
        self.resource(label=label, value='gateway', op=op),

      indexGateway(label='job', op='=~')::
        self.resource(label=label, value='index-gateway', op=op),

      ingester(label='job', op='=~')::
        self.resource(label=label, value='ingester', op=op),

      ingesterZoneAware(label='job', op='=~')::
        self.resource(label=label, value='ingester-zone-aware', op=op),

      overridesExporter(label='job', op='=~')::
        self.resource(label=label, value='overrides-exporter', op=op),

      partitionIngester(label='job', op='=~')::
        self.resource(label=label, value='partition-ingester', op=op),

      patternIngester(label='job', op='=~')::
        self.resource(label=label, value='pattern-ingester', op=op),

      querier(label='job', op='=~')::
        self.resource(label=label, value='querier', op=op),

      queryFrontend(label='job', op='=~')::
        self.resource(label=label, value='query-frontend', op=op),

      queryScheduler(label='job', op='=~')::
        self.resource(label=label, value='query-scheduler', op=op),

      ruler(label='job', op='=~')::
        self.resource(label=label, value='ruler', op=op),

      // Adds multiple label selectors from a key-value object
      withLabels(labels):: self {
        _labels+:: [{ label: k, op: '=', value: labels[k] } for k in std.objectFields(labels)],
      },

      // Adds a single label selector with specified operator and value
      withLabel(label, op='=', value=null, metadata={}):: self {
        // add the label to the list of labels
        _labels+:: [
          if std.type(label) == 'object' then
            metadata + label
          else
            metadata + { label: label, op: op, value: value },
        ],
      },

      // get the final label selector array with only the last occurrence of each label+op and only the label, op and value properties
      list()::
        // Reverse the array and filter out duplicates by label+op, keeping only the last occurrence
        // duplicates are possible if the shorthand components are used i.e. selector().querier().queryFrontend().queryScheduler()
        // and we don't want the same label/op/value to be rendered multiple times
        std.reverse(
          std.foldl(
            function(acc, l)
              // Only add label if we haven't seen this label+op combo yet
              if !std.member(acc, l) && !std.foldl(
                function(found, x) found || (x.label == l.label && x.op == l.op),
                acc,
                false
              )
              then acc + [{label: l.label, op: l.op, value: l.value}]
              else acc,
            std.reverse(self._labels), // Reverse array to process last items first
            [] // Start with empty accumulator
          )
        ),

      // Builds the final label selector expression string
      build(brackets=false)::
        local selectorString = (
          if self._labels == [] then
            ''
          else
            // build the selector string
            std.join(
              ', ',
              // remove duplicates
              std.uniq(
                // sort the labels
                std.sort(
                  // loop over each of the labels and build the matcher string
                  [
                    std.format(
                      '%s%s"%s"',
                      [l.label, l.op, l.value]
                    )

                    for l in self.list()
                  ]
                )
              )
            )
          );
        if brackets then
          '{' + selectorString + '}'
        else
          selectorString,
    };

    // Return either base-initialized or empty selector
    if includeBase then
      selector.cluster().namespace()
    else
      selector,

  debug(obj)::
    std.trace(std.toString(obj), obj),

  toCamelCase(str)::
    local parts = std.split(str, '-');
    parts[0] + std.join('', [
      std.asciiUpper(std.substr(part, 0, 1)) +
      std.substr(part, 1, std.length(part) - 1)
      for part in parts[1:]
    ]),
}
