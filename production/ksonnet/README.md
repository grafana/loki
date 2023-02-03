# Deploy Loki to Kubernetes

See the [Tanka Installation Docs](../../docs/sources/installation/tanka.md)

##  Multizone ingesters
To use multizone ingesters use following config fields
   ```
    _config+: {
        multi_zone+: {
            enabled: false,
            migration_enabled: false,
            num_replicas: 0,
            max_unavailable: 25,
        }
   }
   ```
