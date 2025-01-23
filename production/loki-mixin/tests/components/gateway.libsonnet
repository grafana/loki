{
  name: 'Gateway Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a gateway job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('gateway').build(),
          expected: 'job=~"($namespace)/(gateway)"',
        },
        {
          name: 'supports building a gateway job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)"',
        },
        {
          name: 'supports building a gateway job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(gateway|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(gateway|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(gateway)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a gateway pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('gateway').build(),
          expected: 'pod=~"((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('gateway').build(),
          expected: 'cluster="$cluster", pod=~"((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((gateway|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((gateway|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a gateway container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('gateway').build(),
          expected: 'cluster="$cluster", container=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway(label='container').build(),
          expected: 'cluster="$cluster", container=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('gateway').build(),
          expected: 'container=~"(gateway)"',
        },
        {
          name: 'supports building a gateway container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('gateway').build(),
          expected: 'cluster="$cluster", container=~"(gateway)"',
        },
        {
          name: 'supports building a gateway container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['gateway']).build(),
          expected: 'cluster="$cluster", container=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(gateway|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(gateway|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(gateway)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a gateway component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('gateway').build(),
          expected: 'cluster="$cluster", component=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway(label='component').build(),
          expected: 'cluster="$cluster", component=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('gateway').build(),
          expected: 'component=~"(gateway)"',
        },
        {
          name: 'supports building a gateway component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('gateway').build(),
          expected: 'cluster="$cluster", component=~"(gateway)"',
        },
        {
          name: 'supports building a gateway component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['gateway']).build(),
          expected: 'cluster="$cluster", component=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(gateway|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(gateway|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(gateway)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a gateway selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a gateway selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='gateway').build(),
          expected: 'cluster="$cluster", container=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['gateway']).build(),
          expected: 'cluster="$cluster", container=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='gateway').build(),
          expected: 'cluster="$cluster", component=~"(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['gateway']).build(),
          expected: 'cluster="$cluster", component=~"(gateway)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a gateway route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a gateway route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a gateway selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a gateway selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(gateway)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a gateway selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a gateway selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().gateway()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(gateway)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
