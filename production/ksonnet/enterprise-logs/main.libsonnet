// TODO(jdb): Use cluster_dns_suffix to configure absolute domain names so as to avoid excessive lookups.
// TODO(jdb): Introduce utility functions for mapping over all containers, all microservices (modules), all apps, etc..
local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet',
      clusterRole = k.rbac.v1.clusterRole,
      clusterRoleBinding = k.rbac.v1.clusterRoleBinding,
      container = k.core.v1.container,
      configMap = k.core.v1.configMap,
      deployment = k.apps.v1.deployment,
      job = k.batch.v1.job,
      policyRule = k.rbac.v1.policyRule,
      pvc = k.core.v1.persistentVolumeClaim,
      service = k.core.v1.service,
      serviceAccount = k.core.v1.serviceAccount,
      subject = k.rbac.v1.subject,
      statefulSet = k.apps.v1.statefulSet;
local loki = import 'github.com/grafana/loki/production/ksonnet/loki/loki.libsonnet';
local util = (import 'github.com/grafana/jsonnet-libs/ksonnet-util/util.libsonnet').withK(k) {
  withNonRootSecurityContext(uid, fsGroup=null)::
    { spec+: { template+: { spec+: { securityContext: {
      fsGroup: if fsGroup == null then uid else fsGroup,
      runAsNonRoot: true,
      runAsUser: uid,
    } } } } },
};

loki {
  _config+:: {
    commonArgs+:: {
      'auth.enabled': 'true',
      'auth.type': 'enterprise',
      'cluster-name':
        if self['auth.type'] == 'enterprise' then
          error 'must set _config.commonArgs.cluster-name'
        else null,
    },

    loki+: {
      distributor+: {
        ring: {
          kvstore: {
            store: 'memberlist',
          },
        },
      },
      ingester+: {
        lifecycler+: {
          ring: {
            kvstore: {
              store: 'memberlist',
            },
          },
        },
      },
      memberlist: {
        retransmit_factor: 2,
        gossip_interval: '5s',
        stream_timeout: '5s',
        abort_if_cluster_join_fails: false,
        bind_port: 7946,
        join_members: ['gossip-ring'],
      },
    },

    ingester_pvc_size: '50Gi',
    stateful_ingesters: true,

    querier_pvc_size: '50Gi',
    stateful_queriers: true,

    compactor_pvc_size: '50Gi',
  },

  _images+:: {
    loki: 'grafana/enterprise-logs:v1.1.0',
    kubectl: 'bitnami/kubectl',
  },

  admin_api_args:: self._config.commonArgs {
    'bootstrap.license.path': '/etc/gel-license/license.jwt',
    target: 'admin-api',
  },
  admin_api_container::
    container.new(name='admin-api', image=self._images.loki)
    + container.withArgs(util.mapToFlags(self.admin_api_args))
    + container.withPorts(loki.util.defaultPorts)
    + container.resources.withLimits({ cpu: '2', memory: '4Gi' })
    + container.resources.withRequests({ cpu: '500m', memory: '1Gi' })
    + loki.util.readinessProbe,
  admin_api_deployment:
    deployment.new(name='admin-api', replicas=3, containers=[self.admin_api_container])
    + deployment.spec.selector.withMatchLabelsMixin({ name: 'admin-api' })
    + deployment.spec.template.metadata.withLabelsMixin({ name: 'admin-api', gossip_ring_member: 'true' })
    + deployment.spec.template.spec.withTerminationGracePeriodSeconds(15)
    + util.configVolumeMount('loki', '/etc/loki/config')
    + util.secretVolumeMount('gel-license', '/etc/gel-license/')
    + util.withNonRootSecurityContext(uid=10001),
  admin_api_service:
    util.serviceFor(self.admin_api_deployment),

  compactor_data_pvc+: { spec+: { storageClassName:: null } },
  compactor_statefulset+: util.withNonRootSecurityContext(10001),

  // Remove consul in favor of memberlist.
  consul_config_map:: {},
  consul_deployment:: null,
  consul_service:: null,

  distributor_deployment+:
    deployment.spec.template.metadata.withLabelsMixin({ gossip_ring_member: 'true' })
    + util.withNonRootSecurityContext(uid=10001),

  gateway_args:: self._config.commonArgs {
    'bootstrap.license.path': '/etc/gel-license/license.jwt',
    'gateway.proxy.admin-api.url': 'http://admin-api:%s' % $._config.http_listen_port,
    'gateway.proxy.compactor.url': 'http://compactor:%s' % $._config.http_listen_port,
    'gateway.proxy.distributor.url': 'dns:///distributor:9095',
    'gateway.proxy.ingester.url': 'http://ingester:%s' % $._config.http_listen_port,
    'gateway.proxy.query-frontend.url': 'http://query-frontend:%s' % $._config.http_listen_port,
    'gateway.proxy.ruler.url': 'http://ruler:%s' % $._config.http_listen_port,
    target: 'gateway',
  },
  gateway_container::
    container.new(name='gateway', image=self._images.loki)
    + container.withArgs(util.mapToFlags(self.gateway_args))
    + container.withPorts(loki.util.defaultPorts)
    + container.resources.withLimits({ cpu: '2', memory: '4Gi' })
    + container.resources.withRequests({ cpu: '500m', memory: '1Gi' })
    + loki.util.readinessProbe,
  gateway_deployment:
    deployment.new(name='gateway', replicas=3, containers=[self.gateway_container])
    + deployment.spec.template.spec.withTerminationGracePeriodSeconds(15)
    + deployment.spec.template.spec.securityContext.withFsGroup(10001)
    + deployment.spec.template.spec.securityContext.withRunAsNonRoot(true)
    + deployment.spec.template.spec.securityContext.withRunAsUser(10001)
    + util.configVolumeMount('loki', '/etc/loki/config')
    + util.withNonRootSecurityContext(uid=10001),
  // Remove the htpasswd Secret used by the OSS Loki NGINX gateway.
  gateway_secret:: null,
  gateway_service:
    util.serviceFor(self.gateway_deployment),

  ingester_data_pvc+: { spec+: { storageClassName:: null } },
  ingester_statefulset+:
    statefulSet.spec.template.metadata.withLabelsMixin({ gossip_ring_member: 'true' })
    + util.withNonRootSecurityContext(uid=10001),

  querier_data_pvc+: { spec+: { storageClassName:: null } },
  querier_statefulset+:
    statefulSet.spec.template.metadata.withLabelsMixin({ gossip_ring_member: 'true' })
    + util.withNonRootSecurityContext(uid=10001),

  table_manager_deployment+: util.withNonRootSecurityContext(uid=10001),

  tokengen_args:: self._config.commonArgs {
    target: 'tokengen',
    'tokengen.token-file': '/shared/admin-token',
  },
  tokengen_container::
    container.new('tokengen', self._images.loki)
    + container.withPorts(loki.util.defaultPorts)
    + container.withArgs(util.mapToFlags(self.tokengen_args))
    + container.withVolumeMounts([
      { mountPath: '/etc/loki/config', name: 'config' },
      { mountPath: '/shared', name: 'shared' },
    ])
    + container.resources.withLimits({ memory: '4Gi' })
    + container.resources.withRequests({ cpu: '500m', memory: '500Mi' }),
  tokengen_create_secret_container::
    container.new('create-secret', self._images.kubectl)
    + container.withCommand([
      '/bin/bash',
      '-euc',
      'kubectl create secret generic gel-admin-token --from-file=token=/shared/admin-token --from-literal=grafana-token="self.base64 <(echo :self.cat /shared/admin-token)))"',
    ])
    + container.withVolumeMounts([{ mountPath: '/shared', name: 'shared' }]),
  tokengen_job::
    job.new('tokengen')
    + job.spec.withCompletions(1)
    + job.spec.withParallelism(1)
    + job.spec.template.spec.withContainers([self.tokengen_create_secret_container])
    + job.spec.template.spec.withInitContainers([self.tokengen_container])
    + job.spec.template.spec.withRestartPolicy('Never')
    + job.spec.template.spec.withServiceAccount('tokengen')
    + job.spec.template.spec.withServiceAccountName('tokengen')
    + job.spec.template.spec.withVolumes([
      { name: 'config', configMap: { name: 'loki' } },
      { name: 'shared', emptyDir: {} },
    ])
    + util.withNonRootSecurityContext(uid=10001),
  tokengen_service_account:
    serviceAccount.new('tokengen'),
  tokengen_cluster_role:
    clusterRole.new('tokengen')
    + clusterRole.withRules([
      policyRule.withApiGroups([''])
      + policyRule.withResources(['secrets'])
      + policyRule.withVerbs(['create']),
    ]),
  tokengen_cluster_role_binding:
    clusterRoleBinding.new()
    + clusterRoleBinding.metadata.withName('tokengen')
    + clusterRoleBinding.roleRef.withApiGroup('rbac.authorization.k8s.io')
    + clusterRoleBinding.roleRef.withKind('ClusterRole')
    + clusterRoleBinding.roleRef.withName('tokengen')
    + clusterRoleBinding.withSubjects([
      subject.new()
      + subject.withName('tokengen')
      + subject.withKind('ServiceAccount')
      + { namespace: $._config.namespace },
    ]),

  gossip_ring_service:
    service.new(
      name='gossip-ring',
      selector={ gossip_ring_member: 'true' },
      ports=[{ name: 'gossip-ring', port: 7946, protocol: 'TCP', targetPort: 7946 }],
    )
    + service.spec.withClusterIp('None')
    + service.spec.withPublishNotReadyAddresses(true),
}
