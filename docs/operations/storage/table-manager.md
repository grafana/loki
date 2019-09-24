# Table Manager

The Table Manager is used to delete old data past a certain retention period.
The Table Manager also includes support for automatically provisioning DynamoDB
tables with autoscaling support.

For detailed information on configuring the Table Manager, refer to the
[table_manager_config](../../configuration/README.md#table_manager_config)
section in the Loki configuration document.

## DynamoDB Provisioning

When configuring DynamoDB with the Table Manager, the default provisioning
capacity units for reads are set to 300 and writes are set to 3000. The defaults
can be overwritten:

```yaml
table_manager:
  index_tables_provisioning:
    provisioned_write_throughput: 10
    provisioned_read_throughput: 10
  chunk_tables_provisioning:
    provisioned_write_throughput: 10
    provisioned_read_throughput: 10
```

If Table Manager is not automatically managing DynamoDB, old data cannot easily
be erased and the index will grow indefinitely. Manual configurations should
ensure that the primary index key is set to `h` (string) and the sort key is set
to `r` (binary). The "period" attribute in the configuration YAML should be set
to `0`.

