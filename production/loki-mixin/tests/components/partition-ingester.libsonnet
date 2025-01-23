{
  name: 'Partition Ingester Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a partition-ingester job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('partition-ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('partition-ingester').build(),
          expected: 'job=~"($namespace)/(partition-ingester.*)"',
        },
        {
          name: 'supports building a partition-ingester job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('partition-ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)"',
        },
        {
          name: 'supports building a partition-ingester job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['partition-ingester']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a partition-ingester pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('partition-ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((partition-ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a partition-ingester pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((partition-ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a partition-ingester pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('partition-ingester').build(),
          expected: 'pod=~"((partition-ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a partition-ingester pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('partition-ingester').build(),
          expected: 'cluster="$cluster", pod=~"((partition-ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a partition-ingester pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['partition-ingester']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((partition-ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a partition-ingester container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('partition-ingester').build(),
          expected: 'cluster="$cluster", container=~"(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester(label='container').build(),
          expected: 'cluster="$cluster", container=~"(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('partition-ingester').build(),
          expected: 'container=~"(partition-ingester.*)"',
        },
        {
          name: 'supports building a partition-ingester container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('partition-ingester').build(),
          expected: 'cluster="$cluster", container=~"(partition-ingester.*)"',
        },
        {
          name: 'supports building a partition-ingester container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['partition-ingester']).build(),
          expected: 'cluster="$cluster", container=~"(partition-ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a partition-ingester component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('partition-ingester').build(),
          expected: 'cluster="$cluster", component=~"(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester(label='component').build(),
          expected: 'cluster="$cluster", component=~"(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('partition-ingester').build(),
          expected: 'component=~"(partition-ingester.*)"',
        },
        {
          name: 'supports building a partition-ingester component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('partition-ingester').build(),
          expected: 'cluster="$cluster", component=~"(partition-ingester.*)"',
        },
        {
          name: 'supports building a partition-ingester component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['partition-ingester']).build(),
          expected: 'cluster="$cluster", component=~"(partition-ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a partition-ingester selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='partition-ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['partition-ingester']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='partition-ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((partition-ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a partition-ingester selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['partition-ingester']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((partition-ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a partition-ingester selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='partition-ingester').build(),
          expected: 'cluster="$cluster", container=~"(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['partition-ingester']).build(),
          expected: 'cluster="$cluster", container=~"(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='partition-ingester').build(),
          expected: 'cluster="$cluster", component=~"(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['partition-ingester']).build(),
          expected: 'cluster="$cluster", component=~"(partition-ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a partition-ingester route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a partition-ingester route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a partition-ingester selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a partition-ingester selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a partition-ingester selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a partition-ingester selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().partitionIngester()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(partition-ingester.*)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
