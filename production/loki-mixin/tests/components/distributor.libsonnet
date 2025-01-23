{
  name: 'Distributor Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a distributor job selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('distributor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor job selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor job selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('distributor').build(),
          expected: 'job=~"($namespace)/(distributor)"',
        },
        {
          name: 'supports building a distributor job selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('distributor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)"',
        },
        {
          name: 'supports building a distributor job selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['distributor']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor job selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('distributor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(distributor|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor job selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('distributor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(distributor|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor job selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('distributor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(distributor|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('distributor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(distributor)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a distributor pod selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('distributor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('distributor').build(),
          expected: 'pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('distributor').build(),
          expected: 'cluster="$cluster", pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['distributor']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('distributor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((distributor|single-binary|write)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('distributor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((distributor|write)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('distributor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((distributor|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('distributor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a distributor container selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('distributor').build(),
          expected: 'cluster="$cluster", container=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor container selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor(label='container').build(),
          expected: 'cluster="$cluster", container=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor container selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('distributor').build(),
          expected: 'container=~"(distributor)"',
        },
        {
          name: 'supports building a distributor container selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('distributor').build(),
          expected: 'cluster="$cluster", container=~"(distributor)"',
        },
        {
          name: 'supports building a distributor container selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['distributor']).build(),
          expected: 'cluster="$cluster", container=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor container selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('distributor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(distributor|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor container selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('distributor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(distributor|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor container selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('distributor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(distributor|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('distributor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(distributor)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a distributor component selector from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('distributor').build(),
          expected: 'cluster="$cluster", component=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor component selector from a string using the shorthand wrapper',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor(label='component').build(),
          expected: 'cluster="$cluster", component=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor component selector without cluster or namespace labels',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('distributor').build(),
          expected: 'component=~"(distributor)"',
        },
        {
          name: 'supports building a distributor component selector without namespace label',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('distributor').build(),
          expected: 'cluster="$cluster", component=~"(distributor)"',
        },
        {
          name: 'supports building a distributor component selector from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['distributor']).build(),
          expected: 'cluster="$cluster", component=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor component selector with meta-monitoring enabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('distributor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(distributor|single-binary|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor component selector with meta-monitoring enabled and loki-single-binary disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('distributor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(distributor|write)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor component selector with meta-monitoring enabled and paths disabled',
          actual:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('distributor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(distributor|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('distributor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(distributor)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a distributor selector for the job label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='distributor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor selector for the job label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['distributor']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor selector for the pod label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='distributor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor selector for the pod label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['distributor']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a distributor selector for the container label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='distributor').build(),
          expected: 'cluster="$cluster", container=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor selector for the container label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['distributor']).build(),
          expected: 'cluster="$cluster", container=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor selector for the component label using the resource() wrapper from a string',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='distributor').build(),
          expected: 'cluster="$cluster", component=~"(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor selector for the component label using the resource() wrapper from an array',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['distributor']).build(),
          expected: 'cluster="$cluster", component=~"(distributor)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a distributor route selector',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a distributor route selector with custom route',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a distributor selector with custom label using regex match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a distributor selector with custom label using regex non-match',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(distributor)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a distributor selector with custom label using equality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor selector with custom label using inequality',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(distributor)", namespace="$namespace"',
        },
        {
          name: 'supports building a distributor selector with multiple custom labels using different operators',
          actual:
            local selector = (import '../../selectors.libsonnet').new;
            selector().distributor()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(distributor)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
