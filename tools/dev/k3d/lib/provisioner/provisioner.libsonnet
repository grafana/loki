local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet',
      container = k.core.v1.container,
      job = k.batch.v1.job,
      persistentVolumeClaim = k.core.v1.persistentVolumeClaim,
      policyRule = k.rbac.v1.policyRule,
      subject = k.rbac.v1.subject,
      role = k.rbac.v1.role,
      roleBinding = k.rbac.v1.roleBinding,
      serviceAccount = k.core.v1.serviceAccount,
      volume = k.core.v1.volume,
      volumeMount = k.core.v1.volumeMount;
{
  local name = 'provisioner',

  _images+:: {
    kubectl: 'bitnami/kubectl:1.18.0',
    provisioner: 'grafana/enterprise-logs-provisioner',
  },

  _config+:: {
    adminTokenSecret: error 'please provide $._config.adminTokenSecret',
    adminApiUrl: error 'adminApiUrl must be provided as $._config.adminApiUrl',
    volumeMounts: [
      volumeMount.new('shared', '/bootstrap'),
      volumeMount.new('token', '/bootstrap/token', readOnly=true) +
      volumeMount.withSubPath('token'),
    ],
    provisioner: {
      initCommand: [],
      containerCommand: [],
    },
  },

  provisioner: {
    initContainer::
      container.new(name, $._images.provisioner)
      + container.withCommand($._config.provisioner.initCommand)
      + container.withVolumeMounts($._config.volumeMounts),

    container::
      container.new(name + '-create-secret', $._images.kubectl)
      + container.withCommand($._config.provisioner.containerCommand)
      + container.withVolumeMounts($._config.volumeMounts),

    job:
      job.new(name)
      + job.spec.withCompletions(1)
      + job.spec.withParallelism(1)
      + job.spec.template.spec.withInitContainers([self.initContainer])
      + job.spec.template.spec.withContainers([self.container])
      + job.spec.template.spec.withRestartPolicy('OnFailure')
      + job.spec.template.spec.withServiceAccount(name)
      + job.spec.template.spec.withServiceAccountName(name)
      + job.spec.template.spec.withVolumes([
        volume.fromPersistentVolumeClaim('shared', 'provisioner-pv-shared'),
        volume.fromSecret('token', $._config.adminTokenSecret),
      ]),

    persistentVolumeClaim: persistentVolumeClaim.new('provisioner-pv-shared')
                           + persistentVolumeClaim.spec.resources.withRequests({ storage: '100Mi' })
                           + persistentVolumeClaim.spec.withAccessModes(['ReadWriteOnce']),

    serviceAccount:
      serviceAccount.new(name),

    role:
      role.new(name)
      + role.withRules([
        policyRule.withApiGroups([''])
        + policyRule.withResources(['secrets'])
        + policyRule.withVerbs(['create', 'delete']),
      ]),

    roleBinding:
      roleBinding.new()
      + roleBinding.metadata.withName(name)
      + roleBinding.roleRef.withApiGroup('rbac.authorization.k8s.io')
      + roleBinding.roleRef.withKind('Role')
      + roleBinding.roleRef.withName(name)
      + roleBinding.withSubjects([
        subject.new()
        + subject.withName(name)
        + subject.withKind('ServiceAccount'),
      ]),
  },
}
