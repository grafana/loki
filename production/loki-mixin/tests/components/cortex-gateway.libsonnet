{
  name: 'Cortex Gateway Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a cortex-gateway job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('cortex-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('cortex-gateway').build(),
          expected: 'job=~"($namespace)/(cortex-gw(-internal)?)"',
        },
        {
          name: 'supports building a cortex-gateway job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('cortex-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)"',
        },
        {
          name: 'supports building a cortex-gateway job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['cortex-gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a cortex-gateway pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('cortex-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((cortex-gw(-internal)?)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a cortex-gateway pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((cortex-gw(-internal)?)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a cortex-gateway pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('cortex-gateway').build(),
          expected: 'pod=~"((cortex-gw(-internal)?)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a cortex-gateway pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('cortex-gateway').build(),
          expected: 'cluster="$cluster", pod=~"((cortex-gw(-internal)?)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a cortex-gateway pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['cortex-gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((cortex-gw(-internal)?)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a cortex-gateway container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('cortex-gateway').build(),
          expected: 'cluster="$cluster", container=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway(label='container').build(),
          expected: 'cluster="$cluster", container=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('cortex-gateway').build(),
          expected: 'container=~"(cortex-gw(-internal)?)"',
        },
        {
          name: 'supports building a cortex-gateway container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('cortex-gateway').build(),
          expected: 'cluster="$cluster", container=~"(cortex-gw(-internal)?)"',
        },
        {
          name: 'supports building a cortex-gateway container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['cortex-gateway']).build(),
          expected: 'cluster="$cluster", container=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a cortex-gateway component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('cortex-gateway').build(),
          expected: 'cluster="$cluster", component=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway(label='component').build(),
          expected: 'cluster="$cluster", component=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('cortex-gateway').build(),
          expected: 'component=~"(cortex-gw(-internal)?)"',
        },
        {
          name: 'supports building a cortex-gateway component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('cortex-gateway').build(),
          expected: 'cluster="$cluster", component=~"(cortex-gw(-internal)?)"',
        },
        {
          name: 'supports building a cortex-gateway component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['cortex-gateway']).build(),
          expected: 'cluster="$cluster", component=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a cortex-gateway selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='cortex-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['cortex-gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='cortex-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((cortex-gw(-internal)?)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a cortex-gateway selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['cortex-gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((cortex-gw(-internal)?)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a cortex-gateway selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='cortex-gateway').build(),
          expected: 'cluster="$cluster", container=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['cortex-gateway']).build(),
          expected: 'cluster="$cluster", container=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='cortex-gateway').build(),
          expected: 'cluster="$cluster", component=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['cortex-gateway']).build(),
          expected: 'cluster="$cluster", component=~"(cortex-gw(-internal)?)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a cortex-gateway route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a cortex-gateway route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a cortex-gateway selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a cortex-gateway selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a cortex-gateway selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace"',
        },
        {
          name: 'supports building a cortex-gateway selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().cortexGateway()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(cortex-gw(-internal)?)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
