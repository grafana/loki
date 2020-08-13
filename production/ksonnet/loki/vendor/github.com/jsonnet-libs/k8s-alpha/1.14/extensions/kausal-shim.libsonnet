// kausal-shim.libsonnet: mimics internals of the ksonnet-lib API for kausal.libsonnet
//
// importing ourselves here, to avoid receiving kausal patches,
// which we otherwise would (super includes them)
local k = import 'k.libsonnet';

{
  core+: { v1+: {
    container+: {
      envType: k.core.v1.envVar,
      envFromType: k.core.v1.envFromSource {
        new():: {},
      },
      portsType: k.core.v1.containerPort,
      volumeMountsType: k.core.v1.volumeMount,
    },
    pod+: {
      spec+: {
        volumesType: k.core.v1.volume,
      },
    },
    service+: {
      spec+: {
        withClusterIp: self.withClusterIP,
        withLoadBalancerIp: self.withLoadBalancerIP,
        portsType: k.core.v1.servicePort,
      },
    },
  } },

  local appsAffinityPatch = {
    nodeAffinity+: {
      requiredDuringSchedulingIgnoredDuringExecutionType: k.core.v1.nodeSelector {
        new():: {},
        nodeSelectorTermsType: k.core.v1.nodeSelectorTerm {
          new():: {},
          matchFieldsType: k.core.v1.nodeSelectorRequirement,
        },
      },
      preferredDuringSchedulingIgnoredDuringExecutionType: k.core.v1.preferredSchedulingTerm {
        new():: {},
        preferenceType: {
          matchFieldsType: k.core.v1.nodeSelectorRequirement,
        },
      },
    },
    podAntiAffinity+: {
      requiredDuringSchedulingIgnoredDuringExecutionType: k.core.v1.podAffinityTerm {
        new():: {},
      },
    },
  },

  local appsPatch = {
    deployment+: {
      spec+: { template+: { spec+: {
        volumesType: k.core.v1.volume,
        containersType: k.core.v1.container,
        tolerationsType: k.core.v1.toleration {
          new():: {},
        },
        affinity+: appsAffinityPatch,
      } } },
    },
    daemonSet+: {
      new(name=''):: super.new(name),
      spec+: { template+: { spec+: {
        withHostPid:: self.withHostPID,
        tolerationsType: k.core.v1.toleration {
          new():: {},
        },
        affinity+: appsAffinityPatch,
      } } },
    },
    statefulSet+: {
      spec+: { template+: { spec+: {
        volumesType: k.core.v1.volume,
        affinity+: appsAffinityPatch,
        tolerationsType: k.core.v1.toleration {
          new():: {},
        },
        imagePullSecretsType: k.core.v1.localObjectReference {
          new():: {},
        },
      } } },
    },
  },

  apps+: {
    v1+: appsPatch,
    v1beta1+: appsPatch,
  },
  extensions+: {
    v1beta1+: appsPatch {
      ingress+: {
        new(name=''):: super.new(name),
        spec+: {
          rulesType: k.extensions.v1beta1.ingressRule {
            httpType+: { pathsType: k.extensions.v1beta1.httpIngressPath },
          },
        },
      },
    },
  },

  batch+: {
    local patch = {
      new(name=''):: super.new(name),
      mixin+: { spec+: { jobTemplate+: { spec+: { template+: { spec+: {
        imagePullSecretsType: k.core.v1.localObjectReference {
          new():: {},
        },
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
      new(name=''):: super.new(name),
      rulesType: k.rbac.v1beta1.policyRule {
        new():: {},
      },
    },
    role+: role,
    clusterRole+: role,

    local binding = {
      new(name=''):: super.new(name),
      subjectsType: k.rbac.v1beta1.subject {
        new():: {},
      },
    },
    roleBinding+: binding,
    clusterRoleBinding+: binding,

    policyRule+: {
      withNonResourceUrls: self.withNonResourceURLs,
    },
  },
  rbac+: {
    v1+: rbacPatch,
    v1beta1+: rbacPatch,
  },

  policy+: {
    v1beta1+: {
      podDisruptionBudget+: {
        new(name=''):: super.new(name),
      },
      podSecurityPolicy+: {
        new(name=''):: super.new(name),
        mixin+: { spec+: {
          runAsUser+: { rangesType: k.policy.v1beta1.idRange { new():: {} } },
          withHostIpc: self.withHostIPC,
          withHostPid: self.withHostPID,
        } },
      },
    },
  },

  storage+: { v1+: {
    storageClass+: {
      new(name=''):: super.new(name),
    },
  } },

  scheduling+: { v1beta1+: {
    priorityClass+: {
      new(name=''):: super.new(name),
    },
  } },

  admissionregistration+: { v1beta1+: {
    local webhooksType = k.admissionregistration.v1beta1.webhook {
      new():: {},
      rulesType: k.admissionregistration.v1beta1.ruleWithOperations {
        new():: {},
      },
      mixin+: { namespaceSelector+: { matchExpressionsType: {
        new():: {},
        withKey(key):: { key: key },
        withOperator(operator):: { operator: operator },
        withValues(values):: { values: if std.isArray(values) then values else [values] },
      } } },
    },
    mutatingWebhookConfiguration+: {
      new(name=''):: super.new(name),
      webhooksType: webhooksType,
    },
    validatingWebhookConfiguration+: {
      new(name=''):: super.new(name),
      webhooksType: webhooksType,
    },
  } },
}
