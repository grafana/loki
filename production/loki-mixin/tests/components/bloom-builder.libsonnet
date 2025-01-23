{
  name: 'Bloom-Builder Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a bloom-builder job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('bloom-builder').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('bloom-builder').build(),
          expected: 'job=~"($namespace)/(bloom-builder)"',
        },
        {
          name: 'supports building a bloom-builder job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('bloom-builder').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)"',
        },
        {
          name: 'supports building a bloom-builder job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['bloom-builder']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder job selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('bloom-builder').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-builder|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder job selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('bloom-builder').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder job selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('bloom-builder').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-builder|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('bloom-builder').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-builder)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a bloom-builder pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('bloom-builder').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('bloom-builder').build(),
          expected: 'pod=~"((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('bloom-builder').build(),
          expected: 'cluster="$cluster", pod=~"((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['bloom-builder']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('bloom-builder').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-builder|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('bloom-builder').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('bloom-builder').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-builder|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('bloom-builder').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a bloom-builder container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('bloom-builder').build(),
          expected: 'cluster="$cluster", container=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder(label='container').build(),
          expected: 'cluster="$cluster", container=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('bloom-builder').build(),
          expected: 'container=~"(bloom-builder)"',
        },
        {
          name: 'supports building a bloom-builder container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('bloom-builder').build(),
          expected: 'cluster="$cluster", container=~"(bloom-builder)"',
        },
        {
          name: 'supports building a bloom-builder container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['bloom-builder']).build(),
          expected: 'cluster="$cluster", container=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder container selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('bloom-builder').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-builder|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder container selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('bloom-builder').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder container selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('bloom-builder').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-builder|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('bloom-builder').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-builder)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a bloom-builder component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('bloom-builder').build(),
          expected: 'cluster="$cluster", component=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder(label='component').build(),
          expected: 'cluster="$cluster", component=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('bloom-builder').build(),
          expected: 'component=~"(bloom-builder)"',
        },
        {
          name: 'supports building a bloom-builder component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('bloom-builder').build(),
          expected: 'cluster="$cluster", component=~"(bloom-builder)"',
        },
        {
          name: 'supports building a bloom-builder component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['bloom-builder']).build(),
          expected: 'cluster="$cluster", component=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder component selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('bloom-builder').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-builder|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder component selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('bloom-builder').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder component selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('bloom-builder').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-builder|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('bloom-builder').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-builder)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a bloom-builder selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='bloom-builder').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['bloom-builder']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='bloom-builder').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['bloom-builder']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-builder)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-builder selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='bloom-builder').build(),
          expected: 'cluster="$cluster", container=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['bloom-builder']).build(),
          expected: 'cluster="$cluster", container=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='bloom-builder').build(),
          expected: 'cluster="$cluster", component=~"(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['bloom-builder']).build(),
          expected: 'cluster="$cluster", component=~"(bloom-builder)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a bloom-builder route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a bloom-builder route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a bloom-builder selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a bloom-builder selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-builder)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a bloom-builder selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(bloom-builder)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-builder selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomBuilder()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(bloom-builder)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
