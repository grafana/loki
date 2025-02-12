{
  name: 'compactor Selector Tests',
  tests: [
    {
      name: 'job selector tests',
      cases: [
        {
          name: 'supports building a compactor job selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job('compactor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor job selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor(label='job').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor job selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).job('compactor').build(),
          expected: 'job=~"($namespace)/(compactor)"',
        },
        {
          name: 'supports building a compactor job selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().job('compactor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)"',
        },
        {
          name: 'supports building a compactor job selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().job(['compactor']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor job selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().job('compactor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(backend|compactor|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor job selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().job('compactor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(backend|compactor|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor job selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().job('compactor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(compactor|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor job selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().job('compactor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/((loki|enterprise-logs)-)?(compactor|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'pod selector tests',
      cases: [
        {
          name: 'supports building a compactor pod selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod('compactor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((compactor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor(label='pod').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((compactor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).pod('compactor').build(),
          expected: 'pod=~"((compactor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().pod('compactor').build(),
          expected: 'cluster="$cluster", pod=~"((compactor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().pod(['compactor']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((compactor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().pod('compactor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((backend|compactor|loki|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().pod('compactor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((backend|compactor|loki)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().pod('compactor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((compactor|loki|single-binary)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor pod selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().pod('compactor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((loki|enterprise-logs)-)?((compactor|loki)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'container selector tests',
      cases: [
        {
          name: 'supports building a compactor container selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container('compactor').build(),
          expected: 'cluster="$cluster", container=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor container selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor(label='container').build(),
          expected: 'cluster="$cluster", container=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor container selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).container('compactor').build(),
          expected: 'container=~"(compactor)"',
        },
        {
          name: 'supports building a compactor container selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().container('compactor').build(),
          expected: 'cluster="$cluster", container=~"(compactor)"',
        },
        {
          name: 'supports building a compactor container selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().container(['compactor']).build(),
          expected: 'cluster="$cluster", container=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor container selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().container('compactor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(backend|compactor|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor container selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().container('compactor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(backend|compactor|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor container selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().container('compactor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(compactor|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor container selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().container('compactor').build(),
          expected: 'cluster="$cluster", container=~"((loki|enterprise-logs)-)?(compactor|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'component selector tests',
      cases: [
        {
          name: 'supports building a compactor component selector from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component('compactor').build(),
          expected: 'cluster="$cluster", component=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor component selector from a string using the shorthand wrapper',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor(label='component').build(),
          expected: 'cluster="$cluster", component=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor component selector without cluster or namespace labels',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).component('compactor').build(),
          expected: 'component=~"(compactor)"',
        },
        {
          name: 'supports building a compactor component selector without namespace label',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector(false).cluster().component('compactor').build(),
          expected: 'cluster="$cluster", component=~"(compactor)"',
        },
        {
          name: 'supports building a compactor component selector from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().component(['compactor']).build(),
          expected: 'cluster="$cluster", component=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor component selector with meta-monitoring enabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                },
              },
            }.new;
            selector().component('compactor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(backend|compactor|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor component selector with meta-monitoring enabled and loki-single-binary disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_sb: false,
                },
              },
            }.new;
            selector().component('compactor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(backend|compactor|loki)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor component selector with meta-monitoring enabled and paths disabled',
          test:
            local selector = (import '../../selectors.libsonnet') {
              _config+:: {
                meta_monitoring+: {
                  enabled: true,
                  include_path: false,
                },
              },
            }.new;
            selector().component('compactor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(compactor|loki|single-binary)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor component selector with meta-monitoring enabled and both paths and single-binary disabled',
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
            selector().component('compactor').build(),
          expected: 'cluster="$cluster", component=~"((loki|enterprise-logs)-)?(compactor|loki)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'resource selector tests',
      cases: [
        {
          name: 'supports building a compactor selector for the job label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value='compactor').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor selector for the job label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='job', value=['compactor']).build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor selector for the pod label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value='compactor').build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((compactor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor selector for the pod label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='pod', value=['compactor']).build(),
          expected: 'cluster="$cluster", namespace="$namespace", pod=~"((compactor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports building a compactor selector for the container label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value='compactor').build(),
          expected: 'cluster="$cluster", container=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor selector for the container label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='container', value=['compactor']).build(),
          expected: 'cluster="$cluster", container=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor selector for the component label using the resource() wrapper from a string',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value='compactor').build(),
          expected: 'cluster="$cluster", component=~"(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor selector for the component label using the resource() wrapper from an array',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().resource(label='component', value=['compactor']).build(),
          expected: 'cluster="$cluster", component=~"(compactor)", namespace="$namespace"',
        },
      ],
    },
    {
      name: 'route selector tests',
      cases: [
        {
          name: 'supports building a compactor route selector',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor().route().build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace", route=~"$route"',
        },
        {
          name: 'supports building a compactor route selector with custom route',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor().route('/api/v1/push').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace", route=~"/api/v1/push"',
        },
      ],
    },
    {
      name: 'custom label tests',
      cases: [
        {
          name: 'supports building a compactor selector with custom label using regex match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor().label('status').re('success|failed').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace", status=~"success|failed"',
        },
        {
          name: 'supports building a compactor selector with custom label using regex non-match',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor().label('status').nre('error|timeout').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(compactor)", namespace="$namespace", status!~"error|timeout"',
        },
        {
          name: 'supports building a compactor selector with custom label using equality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor().label('env').eq('prod').build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor selector with custom label using inequality',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor().label('env').neq('dev').build(),
          expected: 'cluster="$cluster", env!="dev", job=~"($namespace)/(compactor)", namespace="$namespace"',
        },
        {
          name: 'supports building a compactor selector with multiple custom labels using different operators',
          test:
            local selector = (import '../../selectors.libsonnet').new;
            selector().compactor()
            .label('env').eq('prod')
            .label('status').re('success|warning')
            .label('region').neq('eu-west')
            .label('tier').nre('test|staging')
            .build(),
          expected: 'cluster="$cluster", env="prod", job=~"($namespace)/(compactor)", namespace="$namespace", region!="eu-west", status=~"success|warning", tier!~"test|staging"',
        },
      ],
    },
  ],
}
