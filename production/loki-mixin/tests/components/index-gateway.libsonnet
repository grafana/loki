{
  name: 'index-gateway Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a index-gateway job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('index-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('index-gateway').build(),
          expected: 'job=~"($namespace)/(index-gateway)"',
        },
        {
          name: 'supports building a index-gateway job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('index-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)"',
        },
        {
          name: 'supports building a index-gateway job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['index-gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('index-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(backend|index-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('index-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(backend|index-gateway|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('index-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(index-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('index-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(index-gateway|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a index-gateway pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('index-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((index-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((index-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('index-gateway').build(),
          expected: 'pod=~"((index-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('index-gateway').build(),
          expected: 'cluster="$cluster", pod=~"((index-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['index-gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((index-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('index-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((backend|index-gateway|loki|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('index-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((backend|index-gateway|loki)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('index-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((index-gateway|loki|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('index-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((index-gateway|loki)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a index-gateway container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('index-gateway').build(),
          expected: 'cluster="$cluster", container=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway(label='container').build(),
          expected: 'cluster="$cluster", container=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('index-gateway').build(),
          expected: 'container=~"(index-gateway)"',
        },
        {
          name: 'supports building a index-gateway container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('index-gateway').build(),
          expected: 'cluster="$cluster", container=~"(index-gateway)"',
        },
        {
          name: 'supports building a index-gateway container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['index-gateway']).build(),
          expected: 'cluster="$cluster", container=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('index-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(backend|index-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('index-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(backend|index-gateway|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('index-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(index-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('index-gateway').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(index-gateway|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a index-gateway component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('index-gateway').build(),
          expected: 'cluster="$cluster", component=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway(label='component').build(),
          expected: 'cluster="$cluster", component=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('index-gateway').build(),
          expected: 'component=~"(index-gateway)"',
        },
        {
          name: 'supports building a index-gateway component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('index-gateway').build(),
          expected: 'cluster="$cluster", component=~"(index-gateway)"',
        },
        {
          name: 'supports building a index-gateway component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['index-gateway']).build(),
          expected: 'cluster="$cluster", component=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('index-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(backend|index-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('index-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(backend|index-gateway|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('index-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(index-gateway|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('index-gateway').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(index-gateway|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a index-gateway selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='index-gateway').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['index-gateway']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='index-gateway').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((index-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['index-gateway']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((index-gateway)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a index-gateway selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='index-gateway').build(),
          expected: 'cluster="$cluster", container=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['index-gateway']).build(),
          expected: 'cluster="$cluster", container=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='index-gateway').build(),
          expected: 'cluster="$cluster", component=~"(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['index-gateway']).build(),
          expected: 'cluster="$cluster", component=~"(index-gateway)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a index-gateway route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a index-gateway route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a index-gateway selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a index-gateway selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(index-gateway)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a index-gateway selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(index-gateway)", namespace="$namespace"',
        },
        {
          name: 'supports building a index-gateway selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().indexGateway()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(index-gateway)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
