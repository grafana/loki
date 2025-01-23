{
  name: 'Ruler Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a ruler job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('ruler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('ruler').build(),
          expected: 'job=~"($namespace)/(ruler)"',
        },
        {
          name: 'supports building a ruler job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('ruler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)"',
        },
        {
          name: 'supports building a ruler job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['ruler']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler job selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('ruler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(read|ruler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler job selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('ruler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(read|ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler job selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('ruler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ruler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('ruler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ruler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a ruler pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('ruler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('ruler').build(),
          expected: 'pod=~"((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('ruler').build(),
          expected: 'cluster="$cluster", pod=~"((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['ruler']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('ruler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((read|ruler|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('ruler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((read|ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('ruler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ruler|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('ruler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a ruler container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('ruler').build(),
          expected: 'cluster="$cluster", container=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler(label='container').build(),
          expected: 'cluster="$cluster", container=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('ruler').build(),
          expected: 'container=~"(ruler)"',
        },
        {
          name: 'supports building a ruler container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('ruler').build(),
          expected: 'cluster="$cluster", container=~"(ruler)"',
        },
        {
          name: 'supports building a ruler container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['ruler']).build(),
          expected: 'cluster="$cluster", container=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler container selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('ruler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(read|ruler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler container selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('ruler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(read|ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler container selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('ruler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ruler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('ruler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ruler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a ruler component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('ruler').build(),
          expected: 'cluster="$cluster", component=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler(label='component').build(),
          expected: 'cluster="$cluster", component=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('ruler').build(),
          expected: 'component=~"(ruler)"',
        },
        {
          name: 'supports building a ruler component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('ruler').build(),
          expected: 'cluster="$cluster", component=~"(ruler)"',
        },
        {
          name: 'supports building a ruler component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['ruler']).build(),
          expected: 'cluster="$cluster", component=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler component selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('ruler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(read|ruler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler component selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('ruler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(read|ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler component selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('ruler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ruler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('ruler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ruler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a ruler selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='ruler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['ruler']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='ruler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['ruler']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ruler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ruler selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='ruler').build(),
          expected: 'cluster="$cluster", container=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['ruler']).build(),
          expected: 'cluster="$cluster", container=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='ruler').build(),
          expected: 'cluster="$cluster", component=~"(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['ruler']).build(),
          expected: 'cluster="$cluster", component=~"(ruler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a ruler route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a ruler route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a ruler selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a ruler selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ruler)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a ruler selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(ruler)", namespace="$namespace"',
        },
        {
          name: 'supports building a ruler selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ruler()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(ruler)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
