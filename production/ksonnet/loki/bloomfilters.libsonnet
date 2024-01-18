{
  _config+:: {
    // globally enable/disable bloom gateway and bloom compactor
    use_bloom_filters: false,
  },
}
+ (import 'bloom-compactor.libsonnet')
+ (import 'bloom-gateway.libsonnet')
