{
  name: 'ingester-zone Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a ingester-zone job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('ingester-zone').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('ingester-zone').build(),
          expected: 'job=~"($namespace)/(ingester-zone.*)"',
        },
        {
          name: 'supports building a ingester-zone job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('ingester-zone').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)"',
        },
        {
          name: 'supports building a ingester-zone job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['ingester-zone']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('ingester-zone').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester-zone.*|loki|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('ingester-zone').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester-zone.*|loki|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('ingester-zone').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester-zone.*|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('ingester-zone').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(ingester-zone.*|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a ingester-zone pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('ingester-zone').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester-zone.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester-zone.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('ingester-zone').build(),
          expected: 'pod=~"((ingester-zone.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('ingester-zone').build(),
          expected: 'cluster="$cluster", pod=~"((ingester-zone.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['ingester-zone']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester-zone.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('ingester-zone').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester-zone.*|loki|single-binary|write)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('ingester-zone').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester-zone.*|loki|write)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('ingester-zone').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester-zone.*|loki|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('ingester-zone').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((ingester-zone.*|loki)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a ingester-zone container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('ingester-zone').build(),
          expected: 'cluster="$cluster", container=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone(label='container').build(),
          expected: 'cluster="$cluster", container=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('ingester-zone').build(),
          expected: 'container=~"(ingester-zone.*)"',
        },
        {
          name: 'supports building a ingester-zone container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('ingester-zone').build(),
          expected: 'cluster="$cluster", container=~"(ingester-zone.*)"',
        },
        {
          name: 'supports building a ingester-zone container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['ingester-zone']).build(),
          expected: 'cluster="$cluster", container=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('ingester-zone').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('ingester-zone').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('ingester-zone').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('ingester-zone').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a ingester-zone component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('ingester-zone').build(),
          expected: 'cluster="$cluster", component=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone(label='component').build(),
          expected: 'cluster="$cluster", component=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('ingester-zone').build(),
          expected: 'component=~"(ingester-zone.*)"',
        },
        {
          name: 'supports building a ingester-zone component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('ingester-zone').build(),
          expected: 'cluster="$cluster", component=~"(ingester-zone.*)"',
        },
        {
          name: 'supports building a ingester-zone component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['ingester-zone']).build(),
          expected: 'cluster="$cluster", component=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('ingester-zone').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('ingester-zone').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('ingester-zone').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('ingester-zone').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(ingester-zone.*|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a ingester-zone selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='ingester-zone').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['ingester-zone']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='ingester-zone').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester-zone.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['ingester-zone']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((ingester-zone.*)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a ingester-zone selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='ingester-zone').build(),
          expected: 'cluster="$cluster", container=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['ingester-zone']).build(),
          expected: 'cluster="$cluster", container=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='ingester-zone').build(),
          expected: 'cluster="$cluster", component=~"(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['ingester-zone']).build(),
          expected: 'cluster="$cluster", component=~"(ingester-zone.*)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a ingester-zone route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a ingester-zone route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a ingester-zone selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a ingester-zone selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a ingester-zone selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace"',
        },
        {
          name: 'supports building a ingester-zone selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().ingesterZone()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(ingester-zone.*)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
