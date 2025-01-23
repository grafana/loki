{
  name: 'Bloom-Planner Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a bloom-planner job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('bloom-planner').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('bloom-planner').build(),
          expected: 'job=~"($namespace)/(bloom-planner)"',
        },
        {
          name: 'supports building a bloom-planner job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('bloom-planner').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)"',
        },
        {
          name: 'supports building a bloom-planner job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['bloom-planner']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner job selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('bloom-planner').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-planner|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner job selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('bloom-planner').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner job selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('bloom-planner').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-planner|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('bloom-planner').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-planner)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a bloom-planner pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('bloom-planner').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('bloom-planner').build(),
          expected: 'pod=~"((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('bloom-planner').build(),
          expected: 'cluster="$cluster", pod=~"((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['bloom-planner']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('bloom-planner').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-planner|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('bloom-planner').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('bloom-planner').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-planner|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('bloom-planner').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a bloom-planner container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('bloom-planner').build(),
          expected: 'cluster="$cluster", container=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner(label='container').build(),
          expected: 'cluster="$cluster", container=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('bloom-planner').build(),
          expected: 'container=~"(bloom-planner)"',
        },
        {
          name: 'supports building a bloom-planner container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('bloom-planner').build(),
          expected: 'cluster="$cluster", container=~"(bloom-planner)"',
        },
        {
          name: 'supports building a bloom-planner container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['bloom-planner']).build(),
          expected: 'cluster="$cluster", container=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner container selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('bloom-planner').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-planner|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner container selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('bloom-planner').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner container selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('bloom-planner').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-planner|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('bloom-planner').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-planner)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a bloom-planner component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('bloom-planner').build(),
          expected: 'cluster="$cluster", component=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner(label='component').build(),
          expected: 'cluster="$cluster", component=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('bloom-planner').build(),
          expected: 'component=~"(bloom-planner)"',
        },
        {
          name: 'supports building a bloom-planner component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('bloom-planner').build(),
          expected: 'cluster="$cluster", component=~"(bloom-planner)"',
        },
        {
          name: 'supports building a bloom-planner component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['bloom-planner']).build(),
          expected: 'cluster="$cluster", component=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner component selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('bloom-planner').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-planner|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner component selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('bloom-planner').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner component selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('bloom-planner').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-planner|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('bloom-planner').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-planner)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a bloom-planner selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='bloom-planner').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['bloom-planner']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='bloom-planner').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['bloom-planner']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-planner)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-planner selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='bloom-planner').build(),
          expected: 'cluster="$cluster", container=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['bloom-planner']).build(),
          expected: 'cluster="$cluster", container=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='bloom-planner').build(),
          expected: 'cluster="$cluster", component=~"(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['bloom-planner']).build(),
          expected: 'cluster="$cluster", component=~"(bloom-planner)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a bloom-planner route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a bloom-planner route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a bloom-planner selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a bloom-planner selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-planner)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a bloom-planner selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(bloom-planner)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-planner selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomPlanner()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(bloom-planner)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
