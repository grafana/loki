{
  name: 'bloom-gateway Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a bloom-gateway job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('bloom-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('bloom-gateway').build(),
          expected: 'job=~"($namespace)/(bloom-gateway)"',
        },
        {
          name: 'supports building a bloom-gateway job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('bloom-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)"',
        },
        {
          name: 'supports building a bloom-gateway job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['bloom-gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('bloom-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('bloom-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-gateway|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('bloom-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('bloom-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(bloom-gateway|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a bloom-gateway pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('bloom-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('bloom-gateway').build(),
          expected: 'pod=~"((bloom-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('bloom-gateway').build(),
          expected: 'cluster="$cluster", pod=~"((bloom-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['bloom-gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('bloom-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-gateway|loki|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('bloom-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-gateway|loki)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('bloom-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-gateway|loki|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('bloom-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((bloom-gateway|loki)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a bloom-gateway container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('bloom-gateway').build(),
          expected: 'cluster="$cluster", container=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway(label='container').build(),
          expected: 'cluster="$cluster", container=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('bloom-gateway').build(),
          expected: 'container=~"(bloom-gateway)"',
        },
        {
          name: 'supports building a bloom-gateway container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('bloom-gateway').build(),
          expected: 'cluster="$cluster", container=~"(bloom-gateway)"',
        },
        {
          name: 'supports building a bloom-gateway container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['bloom-gateway']).build(),
          expected: 'cluster="$cluster", container=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('bloom-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('bloom-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-gateway|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('bloom-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('bloom-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(bloom-gateway|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a bloom-gateway component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('bloom-gateway').build(),
          expected: 'cluster="$cluster", component=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway(label='component').build(),
          expected: 'cluster="$cluster", component=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('bloom-gateway').build(),
          expected: 'component=~"(bloom-gateway)"',
        },
        {
          name: 'supports building a bloom-gateway component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('bloom-gateway').build(),
          expected: 'cluster="$cluster", component=~"(bloom-gateway)"',
        },
        {
          name: 'supports building a bloom-gateway component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['bloom-gateway']).build(),
          expected: 'cluster="$cluster", component=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('bloom-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('bloom-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-gateway|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('bloom-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('bloom-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(bloom-gateway|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a bloom-gateway selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='bloom-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['bloom-gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='bloom-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['bloom-gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((bloom-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a bloom-gateway selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='bloom-gateway').build(),
          expected: 'cluster="$cluster", container=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['bloom-gateway']).build(),
          expected: 'cluster="$cluster", container=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='bloom-gateway').build(),
          expected: 'cluster="$cluster", component=~"(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['bloom-gateway']).build(),
          expected: 'cluster="$cluster", component=~"(bloom-gateway)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a bloom-gateway route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a bloom-gateway route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a bloom-gateway selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a bloom-gateway selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(bloom-gateway)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a bloom-gateway selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(bloom-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a bloom-gateway selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().bloomGateway()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(bloom-gateway)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
