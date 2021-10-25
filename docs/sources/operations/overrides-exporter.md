---
title: "Overrides Exporter"
weight: 20
---

Loki is a multi-tenant system that supports applying limits to each tenant as a mechanism for resource management. The `overrides-exporter` module exposes these limits as Prometheus metrics in order to help operators better understand tenant behavior.

## Context

Configuration updates to tenant limits can be applied to Loki without restart via the [`runtime_config`](../configuration/#runtime-configuration-file) feature.

## Example

The `overrides-exporter` module is disabled by default. We recommend running a single instance per cluster to avoid issues with metric cardinality as the `overrides-exporter` creates ~40 metrics per tenant with overrides configured.

Using an example `runtime.yaml`:

```yaml
overrides:
  "tenant_1":
    ingestion_rate_mb: 10
    max_streams_per_user: 100000
    max_chunks_per_query: 100000
```

Launch an instance of the `overrides-exporter`:

```shell
loki -target=overrides-exporter -runtime-config.file=runtime.yaml -config.file=basic_schema_config.yaml -server.http-listen-port=8080
```

To inspect the tenant limit overrides:

```shell
$ curl -sq localhost:8080/metrics | grep override                                                                                                                                                                                                                          
# HELP loki_overrides Resource limit overrides applied to tenants                                                                                                                                                                                                          
# TYPE loki_overrides gauge                                                                                                                                                                                                                                                
loki_overrides{limit_name="cardinality_limit",user="user1"} 100000                                                                                                                                                                                                         
loki_overrides{limit_name="creation_grace_period",user="user1"} 6e+11                                                                                                                                                                                                      
loki_overrides{limit_name="ingestion_burst_size_mb",user="user1"} 350000                                                                                                                                                                                                   
loki_overrides{limit_name="ingestion_rate_mb",user="user1"} 10                                                                                                                                                                                                         
loki_overrides{limit_name="max_cache_freshness_per_query",user="user1"} 6e+10                                                                                                                                                                                              
loki_overrides{limit_name="max_chunks_per_query",user="user1"} 100000                                                                                                                                                                                                       
loki_overrides{limit_name="max_concurrent_tail_requests",user="user1"} 10                                                                                                                                                                                                  
loki_overrides{limit_name="max_entries_limit_per_query",user="user1"} 5000                                                                                                                                                                                                 
loki_overrides{limit_name="max_global_streams_per_user",user="user1"} 5000                                                                                                                                                                                                 
loki_overrides{limit_name="max_label_name_length",user="user1"} 1024                                                                                                                                                                                                       
loki_overrides{limit_name="max_label_names_per_series",user="user1"} 30                                                                                                                                                                                                    
loki_overrides{limit_name="max_label_value_length",user="user1"} 2048                                                                                                                                                                                                      
loki_overrides{limit_name="max_line_size",user="user1"} 0                                                                                                                                                                                                                  
loki_overrides{limit_name="max_queriers_per_tenant",user="user1"} 0                                                                                                                                                                                                        
loki_overrides{limit_name="max_query_length",user="user1"} 2.5956e+15                                                                                                                                                                                                      
loki_overrides{limit_name="max_query_lookback",user="user1"} 0                                                                                                                                                                                                             
loki_overrides{limit_name="max_query_parallelism",user="user1"} 32                                                                                                                                                                                                         
loki_overrides{limit_name="max_query_series",user="user1"} 1000                                                                                                                                                                                                            
loki_overrides{limit_name="max_streams_matchers_per_query",user="user1"} 1000                                                                                                                                                                                              
loki_overrides{limit_name="max_streams_per_user",user="user1"} 100000                                                                                                                                                                                                           
loki_overrides{limit_name="min_sharding_lookback",user="user1"} 0                                                                                                                                                                                                          
loki_overrides{limit_name="per_stream_rate_limit",user="user1"} 3.145728e+06                                                                                                                                                                                               
loki_overrides{limit_name="per_stream_rate_limit_burst",user="user1"} 1.572864e+07                                                                                                                                                                                         
loki_overrides{limit_name="per_tenant_override_period",user="user1"} 1e+10                                                                                                                                                                                                 
loki_overrides{limit_name="reject_old_samples_max_age",user="user1"} 1.2096e+15                                                                                                                                                                                            
loki_overrides{limit_name="retention_period",user="user1"} 2.6784e+15                                                                                                                                                                                                      
loki_overrides{limit_name="ruler_evaluation_delay_duration",user="user1"} 0                                                                                                                                                                                                
loki_overrides{limit_name="ruler_max_rule_groups_per_tenant",user="user1"} 0                                                                                                                                                                                               
loki_overrides{limit_name="ruler_max_rules_per_rule_group",user="user1"} 0                                                                                                                                                                                                 
loki_overrides{limit_name="ruler_remote_write_queue_batch_send_deadline",user="user1"} 0                                                                                                                                                                                   
loki_overrides{limit_name="ruler_remote_write_queue_capacity",user="user1"} 0                                                                                                                                                                                              
loki_overrides{limit_name="ruler_remote_write_queue_max_backoff",user="user1"} 0                                                                                                                                                                                           
loki_overrides{limit_name="ruler_remote_write_queue_max_samples_per_send",user="user1"} 0                                                                                                                                                                                  
loki_overrides{limit_name="ruler_remote_write_queue_max_shards",user="user1"} 0                                                                                                                                                                                            
loki_overrides{limit_name="ruler_remote_write_queue_min_backoff",user="user1"} 0                                                                                                                                                                                           
loki_overrides{limit_name="ruler_remote_write_queue_min_shards",user="user1"} 0                                                                                                                                                                                            
loki_overrides{limit_name="ruler_remote_write_timeout",user="user1"} 0                                                                                                                                                                                                     
loki_overrides{limit_name="split_queries_by_interval",user="user1"} 0 
```

Alerts can be created based on these metrics to inform operators when tenants are close to hitting their limits allowing for increases to be applied before the tenant limits are exceeded.
