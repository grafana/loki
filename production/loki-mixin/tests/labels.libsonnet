{
  name: 'Label Selector Tests',
  tests: [
    {
      name: 'cluster label tests',
      cases: [
        {
          name: 'supports changing the cluster label',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  cluster: 'k8s_cluster',
                },
              },
            }.new;
            selector(false).cluster().build(),
          expected: 'k8s_cluster="$cluster"',
        },
        {
          name: 'supports changing the cluster operator',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  cluster: 'k8s_cluster',
                },
              },
            }.new;
            selector(false).cluster(op='=~').build(),
          expected: 'k8s_cluster=~"$cluster"',
        },
        {
          name: 'supports specifying a cluster value',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  cluster: 'k8s_cluster',
                },
              },
            }.new;
            selector(false).cluster(value='my-cluster').build(),
          expected: 'k8s_cluster="my-cluster"',
        },
        {
          name: 'supports changing the cluster label, operator and specifying a cluster value',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  cluster: 'k8s_cluster',
                },
              },
            }.new;
            selector(false).cluster(op='=~', value='my-cluster').build(),
          expected: 'k8s_cluster=~"my-cluster"',
        },
      ],
    },
    {
      name: 'namespace label tests',
      cases: [
        {
          name: 'supports changing the namespace label',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  namespace: 'k8s_namespace',
                },
              },
            }.new;
            selector(false).namespace().build(),
          expected: 'k8s_namespace="$namespace"',
        },
        {
          name: 'supports changing the namespace operator',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  namespace: 'k8s_namespace',
                },
              },
            }.new;
            selector(false).namespace(op='=~').build(),
          expected: 'k8s_namespace=~"$namespace"',
        },
        {
          name: 'supports specifying a namespace value',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  namespace: 'k8s_namespace',
                },
              },
            }.new;
            selector(false).namespace(value='my-namespace').build(),
          expected: 'k8s_namespace="my-namespace"',
        },
        {
          name: 'supports changing the namespace label, operator and specifying a namespace value',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  namespace: 'k8s_namespace',
                },
              },
            }.new;
            selector(false).namespace(op='=~', value='my-namespace').build(),
          expected: 'k8s_namespace=~"my-namespace"',
        },
      ],
    },
    {
      name: 'pod label tests',
      cases: [
        {
          name: 'supports changing the pod label',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  pod: 'k8s_pod',
                },
              },
            }.new;
            selector(false).pod(pods='distributor').build(),
          expected: 'k8s_pod=~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports changing the pod operator',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector(false).label('app').eq('foo').pod(pods='distributor', op='!~').build(),
          expected: 'app="foo", pod!~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
        {
          name: 'supports changing the pod label and operator',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  pod: 'k8s_pod',
                },
              },
            }.new;
            selector(false).label('app').eq('foo').pod(pods='distributor', op='!~').build(),
          expected: 'app="foo", k8s_pod!~"((distributor)-([0-9]+|[a-z0-9]{10}-[a-z0-9]{5}))"',
        },
      ],
    },
    {
      name: 'job label tests',
      cases: [
        {
          name: 'supports changing the job label',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  job: 'k8s_job',
                },
              },
            }.new;
            selector(false).job(jobs='distributor').build(),
          expected: 'k8s_job=~"($namespace)/(distributor)"',
        },
        {
          name: 'supports changing the job operator',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector(false).label('app').eq('foo').job(jobs='distributor', op='!~').build(),
          expected: 'app="foo", job!~"($namespace)/(distributor)"',
        },
        {
          name: 'supports changing the job label and operator',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  job: 'k8s_job',
                },
              },
            }.new;
            selector(false).label('app').eq('foo').job(jobs='distributor', op='!~').build(),
          expected: 'app="foo", k8s_job!~"($namespace)/(distributor)"',
        },
      ],
    },
    {
      name: 'container label tests',
      cases: [
        {
          name: 'supports changing the container label',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  container: 'k8s_container',
                },
              },
            }.new;
            selector(false).container(containers='distributor').build(),
          expected: 'k8s_container=~"(distributor)"',
        },
        {
          name: 'supports changing the container operator',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector(false).label('app').eq('foo').container(containers='distributor', op='!~').build(),
          expected: 'app="foo", container!~"(distributor)"',
        },
        {
          name: 'supports changing the container label and operator',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  container: 'k8s_container',
                },
              },
            }.new;
            selector(false).label('app').eq('foo').container(containers='distributor', op='!~').build(),
          expected: 'app="foo", k8s_container!~"(distributor)"',
        },
      ],
    },
    {
      name: 'component label tests',
      cases: [
        {
          name: 'supports changing the component label',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  component: 'k8s_component',
                },
              },
            }.new;
            selector(false).component(components='distributor').build(),
          expected: 'k8s_component=~"(distributor)"',
        },
        {
          name: 'supports changing the component operator',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector(false).label('app').eq('foo').component(components='distributor', op='!~').build(),
          expected: 'app="foo", component!~"(distributor)"',
        },
        {
          name: 'supports changing the component label and operator',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  component: 'k8s_component',
                },
              },
            }.new;
            selector(false).label('app').eq('foo').component(components='distributor', op='!~').build(),
          expected: 'app="foo", k8s_component!~"(distributor)"',
        },
      ],
    },
    {
      name: 'node label tests',
      cases: [
        {
          name: 'supports changing the node label',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  node: 'k8s_node',
                },
              },
            }.new;
            selector(false).node().build(),
          expected: 'k8s_node=~"$node"',
        },
        {
          name: 'supports changing the node operator',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector(false).label('app').eq('foo').node(op='!~').build(),
          expected: 'app="foo", instance!~"$node"',
        },
        {
          name: 'supports specifying a node value',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  node: 'k8s_node',
                },
              },
            }.new;
            selector(false).node(value='my-node').build(),
          expected: 'k8s_node=~"my-node"',
        },
        {
          name: 'supports changing the node label, operator and specifying a node value',
          test:
            local selector = (import '../selectors.libsonnet') {
              _config+:: {
                labels+: {
                  node: 'k8s_node',
                },
              },
            }.new;
            selector(false).label('app').eq('foo').node(op='!~', value='my-node').build(),
          expected: 'app="foo", k8s_node!~"my-node"',
        },
      ],
    },
    {
      name: 'invalid label tests',
      cases: [
        {
          name: 'handles empty label name',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector().label('').eq('value').build(),
          expected: 'cluster="$cluster", namespace="$namespace"',
        },
        {
          name: 'handles empty label value',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector().label('mylabel').eq('').build(),
          expected: 'cluster="$cluster", mylabel="", namespace="$namespace"',
        },
      ],
    },
  ],
}
