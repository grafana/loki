{
  name: 'Query-Frontend Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a query-frontend job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('query-frontend').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('query-frontend').build(),
          expected: 'job=~"($namespace)/(query-frontend)"',
        },
        {
          name: 'supports building a query-frontend job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('query-frontend').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)"',
        },
        {
          name: 'supports building a query-frontend job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['query-frontend']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend job selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('query-frontend').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(query-frontend|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend job selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('query-frontend').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(query-frontend|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend job selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('query-frontend').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(query-frontend|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend job selector with meta-monitoring enabled and both paths and single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('query-frontend').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(query-frontend)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a query-frontend pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('query-frontend').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('query-frontend').build(),
          expected: 'pod=~"((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('query-frontend').build(),
          expected: 'cluster="$cluster", pod=~"((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['query-frontend']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('query-frontend').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((query-frontend|read|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('query-frontend').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((query-frontend|read)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('query-frontend').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((query-frontend|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend pod selector with meta-monitoring enabled and both paths and single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('query-frontend').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a query-frontend container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('query-frontend').build(),
          expected: 'cluster="$cluster", container=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend(label='container').build(),
          expected: 'cluster="$cluster", container=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('query-frontend').build(),
          expected: 'container=~"(query-frontend)"',
        },
        {
          name: 'supports building a query-frontend container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('query-frontend').build(),
          expected: 'cluster="$cluster", container=~"(query-frontend)"',
        },
        {
          name: 'supports building a query-frontend container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['query-frontend']).build(),
          expected: 'cluster="$cluster", container=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend container selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('query-frontend').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(query-frontend|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend container selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('query-frontend').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(query-frontend|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend container selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('query-frontend').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(query-frontend|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend container selector with meta-monitoring enabled and both paths and single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('query-frontend').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(query-frontend)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a query-frontend component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('query-frontend').build(),
          expected: 'cluster="$cluster", component=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend(label='component').build(),
          expected: 'cluster="$cluster", component=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('query-frontend').build(),
          expected: 'component=~"(query-frontend)"',
        },
        {
          name: 'supports building a query-frontend component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('query-frontend').build(),
          expected: 'cluster="$cluster", component=~"(query-frontend)"',
        },
        {
          name: 'supports building a query-frontend component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['query-frontend']).build(),
          expected: 'cluster="$cluster", component=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend component selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('query-frontend').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(query-frontend|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend component selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('query-frontend').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(query-frontend|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend component selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('query-frontend').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(query-frontend|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend component selector with meta-monitoring enabled and both paths and single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('query-frontend').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(query-frontend)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a query-frontend selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='query-frontend').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['query-frontend']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='query-frontend').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['query-frontend']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-frontend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-frontend selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='query-frontend').build(),
          expected: 'cluster="$cluster", container=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['query-frontend']).build(),
          expected: 'cluster="$cluster", container=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='query-frontend').build(),
          expected: 'cluster="$cluster", component=~"(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['query-frontend']).build(),
          expected: 'cluster="$cluster", component=~"(query-frontend)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a query-frontend route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a query-frontend route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a query-frontend selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a query-frontend selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-frontend)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a query-frontend selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(query-frontend)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-frontend selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryFrontend()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(query-frontend)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
