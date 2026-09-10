// kausal.libsonnet provides a backwards compatible way as many libraries leverage kausal.libsonnet.
// Ideally util.libsonnet is consumed separately.

(import 'grafana.libsonnet')
+ {
  local this = self,
  _config+:: {
    enable_rbac: true,
    enable_pod_priorities: false,
    namespace: error 'Must define a namespace',
  },

  util+::
    (import 'util.libsonnet').withK(this)
    + {
      rbac(name, rules)::
        if $._config.enable_rbac
        then super.rbac(name, rules, $._config.namespace)
        else {},
      namespacedRBAC(name, rules)::
        if $._config.enable_rbac
        then super.namespacedRBAC(name, rules, $._config.namespace)
        else {},
      podPriority(p):
        if $._config.enable_pod_priorities
        then super.podPriority(p)
        else {},
    },
}
