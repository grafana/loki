# Deploy Loki to Kubernetes

See the [Tanka Installation Docs](../../docs/sources/installation/tanka.md)

##  Multizone ingesters
To use multizone ingesters use following config fields
   ```
    _config+: {
        multi_zone_ingester_enabled: false,
        multi_zone_ingester_migration_enabled: false,
        multi_zone_ingester_replicas: 0,
        multi_zone_ingester_max_unavailable: 25,
   }
   ```
