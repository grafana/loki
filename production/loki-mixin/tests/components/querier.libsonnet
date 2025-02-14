{
  name: 'querier Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a querier job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('querier').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('querier').build(),
          expected: 'job=~"($namespace)/(querier)"',
        },
        {
          name: 'supports building a querier job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('querier').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)"',
        },
        {
          name: 'supports building a querier job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['querier']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('querier').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|querier|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('querier').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|querier|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('querier').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|querier|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier job selector with meta-monitoring enabled and both paths and single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('querier').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|querier)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a querier pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('querier').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('querier').build(),
          expected: 'pod=~"((querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('querier').build(),
          expected: 'cluster="$cluster", pod=~"((querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['querier']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('querier').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|querier|read|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('querier').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|querier|read)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('querier').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|querier|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier pod selector with meta-monitoring enabled and both paths and single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('querier').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a querier container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('querier').build(),
          expected: 'cluster="$cluster", container=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier(label='container').build(),
          expected: 'cluster="$cluster", container=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('querier').build(),
          expected: 'container=~"(querier)"',
        },
        {
          name: 'supports building a querier container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('querier').build(),
          expected: 'cluster="$cluster", container=~"(querier)"',
        },
        {
          name: 'supports building a querier container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['querier']).build(),
          expected: 'cluster="$cluster", container=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('querier').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|querier|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('querier').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|querier|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('querier').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|querier|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier container selector with meta-monitoring enabled and both paths and single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('querier').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|querier)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a querier component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('querier').build(),
          expected: 'cluster="$cluster", component=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier(label='component').build(),
          expected: 'cluster="$cluster", component=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('querier').build(),
          expected: 'component=~"(querier)"',
        },
        {
          name: 'supports building a querier component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('querier').build(),
          expected: 'cluster="$cluster", component=~"(querier)"',
        },
        {
          name: 'supports building a querier component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['querier']).build(),
          expected: 'cluster="$cluster", component=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('querier').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|querier|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('querier').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|querier|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('querier').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|querier|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier component selector with meta-monitoring enabled and both paths and single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('querier').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|querier)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a querier selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='querier').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['querier']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='querier').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['querier']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((querier)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a querier selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='querier').build(),
          expected: 'cluster="$cluster", container=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['querier']).build(),
          expected: 'cluster="$cluster", container=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='querier').build(),
          expected: 'cluster="$cluster", component=~"(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['querier']).build(),
          expected: 'cluster="$cluster", component=~"(querier)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a querier route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a querier route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a querier selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a querier selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(querier)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a querier selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(querier)", namespace="$namespace"',
        },
        {
          name: 'supports building a querier selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().querier()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(querier)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
