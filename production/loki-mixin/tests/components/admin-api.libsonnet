{
  name: 'Admin API Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a admin-api job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('admin-api').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('admin-api').build(),
          expected: 'job=~"($namespace)/(admin-api)"',
        },
        {
          name: 'supports building a admin-api job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('admin-api').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)"',
        },
        {
          name: 'supports building a admin-api job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['admin-api']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api job selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('admin-api').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(admin-api|backend|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api job selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('admin-api').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(admin-api|backend)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api job selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('admin-api').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(admin-api|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('admin-api').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(admin-api)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a admin-api pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('admin-api').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('admin-api').build(),
          expected: 'pod=~"((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('admin-api').build(),
          expected: 'cluster="$cluster", pod=~"((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['admin-api']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('admin-api').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((admin-api|backend|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('admin-api').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((admin-api|backend)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('admin-api').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((admin-api|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('admin-api').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a admin-api container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('admin-api').build(),
          expected: 'cluster="$cluster", container=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi(label='container').build(),
          expected: 'cluster="$cluster", container=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('admin-api').build(),
          expected: 'container=~"(admin-api)"',
        },
        {
          name: 'supports building a admin-api container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('admin-api').build(),
          expected: 'cluster="$cluster", container=~"(admin-api)"',
        },
        {
          name: 'supports building a admin-api container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['admin-api']).build(),
          expected: 'cluster="$cluster", container=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api container selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('admin-api').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(admin-api|backend|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api container selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('admin-api').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(admin-api|backend)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api container selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('admin-api').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(admin-api|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('admin-api').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(admin-api)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a admin-api component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('admin-api').build(),
          expected: 'cluster="$cluster", component=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi(label='component').build(),
          expected: 'cluster="$cluster", component=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('admin-api').build(),
          expected: 'component=~"(admin-api)"',
        },
        {
          name: 'supports building a admin-api component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('admin-api').build(),
          expected: 'cluster="$cluster", component=~"(admin-api)"',
        },
        {
          name: 'supports building a admin-api component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['admin-api']).build(),
          expected: 'cluster="$cluster", component=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api component selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('admin-api').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(admin-api|backend|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api component selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('admin-api').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(admin-api|backend)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api component selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('admin-api').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(admin-api|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('admin-api').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(admin-api)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a admin-api selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='admin-api').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['admin-api']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='admin-api').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['admin-api']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((admin-api)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a admin-api selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='admin-api').build(),
          expected: 'cluster="$cluster", container=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['admin-api']).build(),
          expected: 'cluster="$cluster", container=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='admin-api').build(),
          expected: 'cluster="$cluster", component=~"(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['admin-api']).build(),
          expected: 'cluster="$cluster", component=~"(admin-api)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a admin-api route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a admin-api route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a admin-api selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a admin-api selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(admin-api)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a admin-api selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(admin-api)", namespace="$namespace"',
        },
        {
          name: 'supports building a admin-api selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().adminApi()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(admin-api)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
