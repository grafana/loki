{
  name: 'Query-Scheduler Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a query-scheduler job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('query-scheduler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('query-scheduler').build(),
          expected: 'job=~"($namespace)/(query-scheduler)"',
        },
        {
          name: 'supports building a query-scheduler job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('query-scheduler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)"',
        },
        {
          name: 'supports building a query-scheduler job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['query-scheduler']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('query-scheduler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|query-scheduler|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('query-scheduler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|query-scheduler|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('query-scheduler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|query-scheduler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('query-scheduler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|query-scheduler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a query-scheduler pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('query-scheduler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('query-scheduler').build(),
          expected: 'pod=~"((query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('query-scheduler').build(),
          expected: 'cluster="$cluster", pod=~"((query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['query-scheduler']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('query-scheduler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|query-scheduler|read|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('query-scheduler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|query-scheduler|read)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('query-scheduler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|query-scheduler|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('query-scheduler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a query-scheduler container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('query-scheduler').build(),
          expected: 'cluster="$cluster", container=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler(label='container').build(),
          expected: 'cluster="$cluster", container=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('query-scheduler').build(),
          expected: 'container=~"(query-scheduler)"',
        },
        {
          name: 'supports building a query-scheduler container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('query-scheduler').build(),
          expected: 'cluster="$cluster", container=~"(query-scheduler)"',
        },
        {
          name: 'supports building a query-scheduler container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['query-scheduler']).build(),
          expected: 'cluster="$cluster", container=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('query-scheduler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|query-scheduler|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('query-scheduler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|query-scheduler|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('query-scheduler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|query-scheduler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('query-scheduler').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|query-scheduler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a query-scheduler component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('query-scheduler').build(),
          expected: 'cluster="$cluster", component=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler(label='component').build(),
          expected: 'cluster="$cluster", component=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('query-scheduler').build(),
          expected: 'component=~"(query-scheduler)"',
        },
        {
          name: 'supports building a query-scheduler component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('query-scheduler').build(),
          expected: 'cluster="$cluster", component=~"(query-scheduler)"',
        },
        {
          name: 'supports building a query-scheduler component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['query-scheduler']).build(),
          expected: 'cluster="$cluster", component=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('query-scheduler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|query-scheduler|read|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('query-scheduler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|query-scheduler|read)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('query-scheduler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|query-scheduler|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('query-scheduler').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|query-scheduler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a query-scheduler selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='query-scheduler').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['query-scheduler']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='query-scheduler').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['query-scheduler']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((query-scheduler)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a query-scheduler selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='query-scheduler').build(),
          expected: 'cluster="$cluster", container=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['query-scheduler']).build(),
          expected: 'cluster="$cluster", container=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='query-scheduler').build(),
          expected: 'cluster="$cluster", component=~"(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['query-scheduler']).build(),
          expected: 'cluster="$cluster", component=~"(query-scheduler)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a query-scheduler route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a query-scheduler route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a query-scheduler selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a query-scheduler selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(query-scheduler)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a query-scheduler selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(query-scheduler)", namespace="$namespace"',
        },
        {
          name: 'supports building a query-scheduler selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().queryScheduler()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(query-scheduler)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
