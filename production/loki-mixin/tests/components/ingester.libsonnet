{
  name: 'Ingester Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a ingester job selector from a string using the wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('ingester').build(),
          expected: 'job=~"($namespace)/(ingester.*)"',
        },
        {
          name: 'supports building a ingester job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)"',
        },
        {
          name: 'supports building a ingester job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['ingester']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester job selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester.*|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester job selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester.*|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester job selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester.*|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a ingester pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('ingester').build(),
          expected: 'pod=~"((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('ingester').build(),
          expected: 'cluster="$cluster", pod=~"((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['ingester']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester.*|single-binary|write)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester.*|write)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester.*|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a ingester container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('ingester').build(),
          expected: 'cluster="$cluster", container=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester(label='container').build(),
          expected: 'cluster="$cluster", container=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('ingester').build(),
          expected: 'container=~"(ingester.*)"',
        },
        {
          name: 'supports building a ingester container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('ingester').build(),
          expected: 'cluster="$cluster", container=~"(ingester.*)"',
        },
        {
          name: 'supports building a ingester container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['ingester']).build(),
          expected: 'cluster="$cluster", container=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester container selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('ingester').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester.*|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester container selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('ingester').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester.*|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester container selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('ingester').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester.*|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('ingester').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a ingester component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('ingester').build(),
          expected: 'cluster="$cluster", component=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester(label='component').build(),
          expected: 'cluster="$cluster", component=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('ingester').build(),
          expected: 'component=~"(ingester.*)"',
        },
        {
          name: 'supports building a ingester component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('ingester').build(),
          expected: 'cluster="$cluster", component=~"(ingester.*)"',
        },
        {
          name: 'supports building a ingester component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['ingester']).build(),
          expected: 'cluster="$cluster", component=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester component selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('ingester').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester.*|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester component selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('ingester').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester.*|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester component selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('ingester').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester.*|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('ingester').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a ingester selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='ingester').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['ingester']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='ingester').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['ingester']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='ingester').build(),
          expected: 'cluster="$cluster", container=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['ingester']).build(),
          expected: 'cluster="$cluster", container=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='ingester').build(),
          expected: 'cluster="$cluster", component=~"(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['ingester']).build(),
          expected: 'cluster="$cluster", component=~"(ingester.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a ingester route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a ingester route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a ingester selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a ingester selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester.*)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a ingester selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(ingester.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingester()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(ingester.*)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
