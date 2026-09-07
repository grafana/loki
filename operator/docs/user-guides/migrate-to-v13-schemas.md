# Migrating out of the deprecated BoltDB-based Storage Schema

Loki Operator 0.12 will ship a version of Loki that no longer supports BoltDB-based schemas. As a result, any LokiStack instance that still uses a BoltDB-based schema (v11 or v12) must complete the migration described below before upgrading to Loki Operator 0.12. If a LokiStack still references a BoltDB-based schema after upgrading, the Loki Operator 0.12 will produce an error during reconciliation.

## Migration Process
### Step 1: Detect your current state
Run the following command to inspect the schemas on your LokiStack:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.storage.schemas'
```

You should be in one of these three situations:

| Situation | Command result | Action required |
|-----------|----------------|-----------------|
| BoltDB schemas only | Latest entry is either version: v11 or version: v12 | Go to Step 2a |
| Already on TSDB | Latest entry is version: v13 | Go to Step 2b |


### Step 2: Add the TSDB schema
Complete only one of the following sub-steps (2a or 2b) based on your current state, then proceed to Step 3.
#### Step 2a: Stacks with BoltDB schemas
If your LokiStack is either on v11 or v12 schema, append a new v13 entry with a date in the future while keeping the existing entries. For example using the CLI:
```
oc patch lokistack <lokistack-name> -n <lokistack-namespace> --type json -p '[
  {
    "op": "add",
    "path": "/spec/storage/schemas/-",
    "value": {
      "version": "v13",
      "effectiveDate": "'"$(date -u -d '+1 day' +%Y-%m-%d)"'"
    }
  }
]'
```

>> Note: Do not remove the old schema entries yet. Loki needs them to query data that was written before the TSDB effective date. Once all data written under the old schema has fallen out of the retention window, you can remove the old entries.

After this change, proceed to Step 3 to determine how long to wait before upgrading.
#### Step 2b: Already on TSDB
If your LokiStack already has a v13 schema entry, proceed to Step 3 to determine how long to wait before upgrading.
Step 3: Determine the wait time
From the v13 effectiveDate, Loki writes new data using the v13 (TSDB) schema. Data ingested before that date stays on the previous schema.

Loki Operator 0.12 cannot query schemas older than v13. Before you upgrade, every log still inside your retention window must have been written with v13. If you upgrade earlier, logs that were written with the old schema are no longer queryable.

The wait starts from the v13 effectiveDate, not from when you applied the patch. Use the shortest of the periods that apply:
1. Bucket lifecycle policy, if one is configured on the object storage bucket. That policy’s expiry period is the wait time.
2. LokiStack retention, if it is set:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o jsonpath='{.spec.limits.global.retention.days}'
```
If a value is returned, that is your wait time.

30 days, if neither LokiStack retention nor a bucket lifecycle policy is set.

When X days have passed since your v13 effectiveDate, you can proceed to Step 4.

### Step 4: Verify the active schema is active and queryable
After the wait from Step 3, confirm v13 is active before removing old entries.

Check that the LokiStack has accepted the new schema and is Ready:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.status.conditions'
```

Verify that the recent logs are queryable:
```
TOKEN=$(oc whoami -t)
GATEWAY=$(oc -n <lokistack-namespace> get route <lokistack-name>-gateway-http -o jsonpath='{.spec.host}')
curl -k -H "Authorization: Bearer ${TOKEN}" "https://${GATEWAY}/api/logs/v1/infrastructure/loki/api/v1/query?query={kubernetes_namespace_name=%22<lokistack-namespace>%22}&limit=5"
```

Proceed to Step 5 only after both checks pass.

### Step 5: Removing the old schemas
> [!WARNING]
  Do this only after the wait from Step 3. Loki Operator 0.12 will not reconcile LokiStacks that reference v11 or v12, so those entries must be removed before you upgrade. Removing them without the wait in Step 3 will cause the compactor to fail, since BoltDB indexes can no longer be processed and the compaction will abort entirely. Moreover, logs written under the old schema that have not yet aged out of retention will become un-queryable.

The LokiStack spec must still contain the v13 schema with its original effectiveDate. Do not add a new v13 entry.
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json \
  | jq '.spec.storage.schemas |= map(select(.version == "v13"))' \
  | oc apply -f -
```
Confirm that only v13 remains:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.storage.schemas'
```
After patching, confirm that only v13 remains and that the LokiStack condition is Ready:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.storage.schemas'
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.status.conditions'
```
After this change, you can upgrade to Loki Operator 0.12.

## What happens if you upgrade to 0.12 without migrating
If you upgrade to Loki Operator 0.12 while the LokiStack still references a BoltDB-based schema, Loki Operator will attempt reconciliation but will return an error and thus no changes will be made on the resources managed by the operator. The LokiStack will report a Degraded condition indicating that BoltDB-based schemas are no longer supported. This allows users to downgrade their Loki Operator version to perform this migration before they upgrade.

