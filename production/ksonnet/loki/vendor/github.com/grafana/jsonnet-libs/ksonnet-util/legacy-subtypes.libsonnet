// legacy-subtypes.libsonnet makes first-class entities available as subtypes like in ksonnet-lib.
// It also makes the empty new() functions and camelCased functions available.
// This is largely based on kausal-shim.libsonnet from k8s-libsonnet.
{
  core+: { v1+: {
    container+: {
      envType: $.core.v1.envVar,
      envFromType: $.core.v1.envFromSource,
      portsType: $.core.v1.containerPort,
      volumeMountsType: $.core.v1.volumeMount,
    },
    pod+: {
      spec+: {
        volumesType: $.core.v1.volume,
      },
    },
    service+: {
      spec+: {
        withClusterIp: self.withClusterIP,
        withLoadBalancerIp: self.withLoadBalancerIP,
        portsType: $.core.v1.servicePort,
      },
    },

    envFromSource+: { new():: {} },
    nodeSelector+: { new():: {} },
    nodeSelectorTerm+: { new():: {} },
    podAffinityTerm+: { new():: {} },
    preferredSchedulingTerm+: { new():: {} },
    toleration+: { new():: {} },
    localObjectReference+: { new():: {} },
  } },

  local appsAffinityPatch = {
    nodeAffinity+: {
      requiredDuringSchedulingIgnoredDuringExecutionType: $.core.v1.nodeSelector {
        nodeSelectorTermsType: $.core.v1.nodeSelectorTerm {
          matchFieldsType: $.core.v1.nodeSelectorRequirement,
        },
      },
      preferredDuringSchedulingIgnoredDuringExecutionType: $.core.v1.preferredSchedulingTerm {
        preferenceType: {
          matchFieldsType: $.core.v1.nodeSelectorRequirement,
        },
      },
    },
    podAntiAffinity+: {
      requiredDuringSchedulingIgnoredDuringExecutionType: $.core.v1.podAffinityTerm,
    },
  },

  local appsPatch = {
    deployment+: {
      spec+: { template+: { spec+: {
        volumesType: $.core.v1.volume,
        containersType: $.core.v1.container,
        tolerationsType: $.core.v1.toleration,
        affinity+: appsAffinityPatch,
      } } },
    },
    daemonSet+: {
      spec+: { template+: { spec+: {
        withHostPid:: self.withHostPID,
        tolerationsType: $.core.v1.toleration,
        affinity+: appsAffinityPatch,
      } } },
    },
    statefulSet+: {
      spec+: { template+: { spec+: {
        volumesType: $.core.v1.volume,
        affinity+: appsAffinityPatch,
        tolerationsType: $.core.v1.toleration,
        imagePullSecretsType: $.core.v1.localObjectReference,
      } } },
    },
  },

  apps+: {
    v1+: appsPatch,
    v1beta1+: appsPatch,
  },

  batch+: {
    local patch = {
      mixin+: { spec+: { jobTemplate+: { spec+: { template+: { spec+: {
        imagePullSecretsType: $.core.v1.localObjectReference,
      } } } } } },
    },

    v1+: {
      job+: patch,
      cronJob+: patch,
    },
    v1beta1+: {
      job+: patch,
      cronJob+: patch,
    },
  },


  local rbacPatch = {
    local role = {
      rulesType: $.rbac.v1.policyRule,
    },
    role+: role,
    clusterRole+: role,

    local binding = {
      subjectsType: $.rbac.v1.subject,
    },
    roleBinding+: binding,
    clusterRoleBinding+: binding,
    subject+: { new():: {} },

    policyRule+: {
      new():: {},
      withNonResourceUrls: self.withNonResourceURLs,
    },
  },
  rbac+: {
    v1+: rbacPatch,
    // TODO: the v1beta1 RBAC API has been removed in Kubernetes 1.22 and should get removed once 1.22 is the oldest supported version
    v1beta1+: rbacPatch,
  },

  policy+: {
    v1beta1+: {
      idRange+: { new():: {} },
      podSecurityPolicy+: {
        mixin+: { spec+: {
          runAsUser+: { rangesType: $.policy.v1beta1.idRange },
          withHostIpc: self.withHostIPC,
          withHostPid: self.withHostPID,
        } },
      },
    },
  },

  admissionregistration+: { v1beta1+: {
    webhook+: { new():: {} },
    ruleWithOperations+: { new():: {} },
    local webhooksType = $.admissionregistration.v1beta1.webhook {
      rulesType: $.admissionregistration.v1beta1.ruleWithOperations,
      mixin+: { namespaceSelector+: { matchExpressionsType: {
        new():: {},
        withKey(key):: { key: key },
        withOperator(operator):: { operator: operator },
        withValues(values):: { values: if std.isArray(values) then values else [values] },
      } } },
    },
    mutatingWebhookConfiguration+: {
      webhooksType: webhooksType,
    },
    validatingWebhookConfiguration+: {
      webhooksType: webhooksType,
    },
  } },
}
