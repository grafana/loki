local k = import 'ksonnet-util/kausal.libsonnet';

{
  local with(x) = if $._config.wal_enabled then x else {},

  _config+:: {
    loki+: with({
      ingester+: {
        wal+: {
          enabled: true,
          dir: '/loki/wal',
          replay_memory_ceiling: '7GB',  // should be set upto ~50% of available memory
        },
      },
    }),
  },

  local pvc = k.core.v1.persistentVolumeClaim,

  ingester_wal_pvc:: with(
    pvc.new('ingester-wal') +
    pvc.mixin.spec.resources.withRequests({ storage: '150Gi' }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.ingester_pvc_class)
  ),

  local container = k.core.v1.container,
  local volumeMount = k.core.v1.volumeMount,

  ingester_container+:: with(
    k.util.resourcesRequests('1', '7Gi') +
    k.util.resourcesLimits('2', '14Gi') +
    container.withVolumeMountsMixin([
      volumeMount.new('ingester-wal', $._config.loki.ingester.wal.dir),
    ]),
  ),


  local statefulSet = k.apps.v1.statefulSet,
  ingester_statefulset+: with(
    statefulSet.spec.withVolumeClaimTemplatesMixin($.ingester_wal_pvc),
  ),

}
