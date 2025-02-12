{
  name: 'overrides-exporter Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a overrides-exporter job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('overrides-exporter').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('overrides-exporter').build(),
          expected: 'job=~"($namespace)/(overrides-exporter)"',
        },
        {
          name: 'supports building a overrides-exporter job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('overrides-exporter').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)"',
        },
        {
          name: 'supports building a overrides-exporter job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['overrides-exporter']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('overrides-exporter').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|overrides-exporter|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('overrides-exporter').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('overrides-exporter').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|overrides-exporter|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('overrides-exporter').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(loki|overrides-exporter)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a overrides-exporter pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('overrides-exporter').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('overrides-exporter').build(),
          expected: 'pod=~"((overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('overrides-exporter').build(),
          expected: 'cluster="$cluster", pod=~"((overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['overrides-exporter']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('overrides-exporter').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|overrides-exporter|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('overrides-exporter').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('overrides-exporter').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|overrides-exporter|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('overrides-exporter').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((loki|overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a overrides-exporter container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('overrides-exporter').build(),
          expected: 'cluster="$cluster", container=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter(label='container').build(),
          expected: 'cluster="$cluster", container=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('overrides-exporter').build(),
          expected: 'container=~"(overrides-exporter)"',
        },
        {
          name: 'supports building a overrides-exporter container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('overrides-exporter').build(),
          expected: 'cluster="$cluster", container=~"(overrides-exporter)"',
        },
        {
          name: 'supports building a overrides-exporter container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['overrides-exporter']).build(),
          expected: 'cluster="$cluster", container=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('overrides-exporter').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|overrides-exporter|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('overrides-exporter').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('overrides-exporter').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|overrides-exporter|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('overrides-exporter').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(loki|overrides-exporter)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a overrides-exporter component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('overrides-exporter').build(),
          expected: 'cluster="$cluster", component=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter(label='component').build(),
          expected: 'cluster="$cluster", component=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('overrides-exporter').build(),
          expected: 'component=~"(overrides-exporter)"',
        },
        {
          name: 'supports building a overrides-exporter component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('overrides-exporter').build(),
          expected: 'cluster="$cluster", component=~"(overrides-exporter)"',
        },
        {
          name: 'supports building a overrides-exporter component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['overrides-exporter']).build(),
          expected: 'cluster="$cluster", component=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('overrides-exporter').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|overrides-exporter|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('overrides-exporter').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('overrides-exporter').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|overrides-exporter|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('overrides-exporter').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(loki|overrides-exporter)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a overrides-exporter selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='overrides-exporter').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['overrides-exporter']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='overrides-exporter').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['overrides-exporter']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((overrides-exporter)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a overrides-exporter selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='overrides-exporter').build(),
          expected: 'cluster="$cluster", container=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['overrides-exporter']).build(),
          expected: 'cluster="$cluster", container=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='overrides-exporter').build(),
          expected: 'cluster="$cluster", component=~"(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['overrides-exporter']).build(),
          expected: 'cluster="$cluster", component=~"(overrides-exporter)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a overrides-exporter route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a overrides-exporter route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a overrides-exporter selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a overrides-exporter selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(overrides-exporter)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a overrides-exporter selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(overrides-exporter)", namespace="$namespace"',
        },
        {
          name: 'supports building a overrides-exporter selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().overridesExporter()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(overrides-exporter)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
