{
  local with(x) = if $._config.wal_enabled then x else {},

  _config+:: {
    stateful_ingesters: if $._config.wal_enabled then true else super.stateful_ingesters,
    loki+:with({
      ingester+:{
        // disables transfers when running as statefulsets.
        // pod rolling stragety will always fail transfers
        // and the WAL supersedes this.
        max_transfer_retries: 0,
        wal+: {
          enabled: true,
          dir: '/loki/wal',
          replay_memory_ceiling: '7GB', // should be set upto ~50% of available memory
        },
      },
    }),
  },

  local pvc = $.core.v1.persistentVolumeClaim,

  ingester_wal_pvc:: with(
    pvc.new('ingester-wal') +
    pvc.mixin.spec.resources.withRequests({ storage: '150Gi' }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.ingester_pvc_class)
  ),

  local container = $.core.v1.container,
  local volumeMount = $.core.v1.volumeMount,

  ingester_container+:: with(
    $.util.resourcesRequests('1', '7Gi') +
    $.util.resourcesLimits('2', '14Gi') +
    container.withVolumeMountsMixin([
      volumeMount.new('ingester-wal', $._config.loki.ingester.wal.dir),
    ]),
  ),


  local statefulSet = $.apps.v1.statefulSet,
  ingester_statefulset+: with(
    statefulSet.spec.withVolumeClaimTemplatesMixin($.ingester_wal_pvc),
  ),

}
