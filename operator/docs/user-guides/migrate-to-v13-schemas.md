# Migrating out of the Deprecated BoltDB-Based Storage Schema

Loki Operator 0.12 will ship a version of Loki that no longer supports BoltDB-based schemas. As a result, any LokiStack instance that still uses a BoltDB-based schema (v11 or v12) **must** complete the migration described below before upgrading to Loki Operator 0.12. If a LokiStack still references a BoltDB-based schema after upgrading, the Loki Operator 0.12 will produce an error during reconciliation.

## Migration Process
### Step 1: Detect your current state
Run the following command to inspect the schemas on your LokiStack:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.storage.schemas'
```

You should be in one of these three situations:

| Situation | Command result | Action required |
|-----------|----------------|-----------------|
| Single TSDB schema | Latest and only schema is `version: v13` | Go to Step 2a |
| The latest entry is TSDB, but older schemas are still present | Latest entry is `version: v13`  | Go to Step 2b |
| BoltDB schemas only | Latest entry is either `version: v11` or `version: v12` | Go to Step 2c |


### Step 2: Determine your current state and act
Based on your current state, follow only the sub-step that applies to you.

#### Step 2a: Single TSDB schema
If your LokiStack only has a single v13 schema entry then you are fully migrated and you can proceed to upgrade to Loki Operator 0.12, no further steps required.

#### Step 2b: Already on TSDB but with older schemas
If your LokiStack already has a v13 schema entry but still references older schemas, proceed to Step 3 to determine how long to wait before upgrading.

#### Step 2c: Stacks with BoltDB schemas
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

After this change, proceed to Step 3.

### Step 3: Retention and wait time
From the v13 effectiveDate, Loki writes new data using the v13 (TSDB) schema. Data ingested before that date stays on the previous schema.

Loki Operator 0.12 cannot query schemas older than v13. Before you upgrade, every log still inside your retention window must have been written with v13.

By default, LokiStack does not limit log retention, Logs accumulate indefinitely and never age out. This means you cannot safely assume a fixed wait time unless retention is actually enforced. Before computing how long to wait, make sure retention is properly configured.

#### Step 3a: Check your current configuration
1. Check for a bucket lifecycle policy on your object storage bucket (via your cloud provider's console or CLI).
2. Check the LokiStack retention setting:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o jsonpath='{.spec.limits.global.retention.days}'
```
#### Step 3b: Ensure a retention is configured

To avoid data loss and to complete the migration, your bucket lifecycle policy and LokiStack retention must both be set. In particular the LokiStack retention should be configured to be at least 3 days longer than the bucket lifecycle expiry. This keeps the bucket lifecycle policy as the primary enforcement mechanism while LokiStack retention acts purely as a fallback. The 3-day margin absorbs the delay most cloud providers have between an object's expiry and the lifecycle rule actually deleting it.

If your current configuration already satisfies this (a lifecycle policy exists, and LokiStack retention set and is at least 3 days longer), skip to Step 3c.

Otherwise, configure it now:

1. Set a bucket lifecycle policy on your object storage bucket with an expiry of your choice (pick 30 days if you have no existing requirement as that’s the maximum retention supported by Red Hat). If you already have a lifecycle policy, keep its current expiry, let’s call this value N.
2. Set LokiStack retention to N + 3 days:

```
oc patch lokistack <lokistack-name> -n <lokistack-namespace> --type merge -p '{"spec":{"limits":{"global":{"retention":{"days": <N+3>}}}}}'
```

#### Step 3c: Compute the wait time

The wait starts from the v13 effectiveDate. Once retention is configured, your wait time is simply the LokiStack global retention value:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o jsonpath='{.spec.limits.global.retention.days}'
```

Note: If your LokiStack defines per-tenant retention overrides (.spec.limits.tenants.<tenant-name>.retention.days), check those too:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.limits.tenants | to_entries[] | {tenant: .key, days: .value.retention.days}'
```

If a tenant's override is shorter than the global value, that doesn't shorten your wait, global retention still governs the rest of your data. If a tenant's override is longer than the global retention, use that tenant's value instead.

When the applicable number of days have passed since your v13 effectiveDate, you can proceed to Step 4.


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

After patching, confirm that only v13 remains and that the LokiStack condition is Ready:
```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.storage.schemas'
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.status.conditions'
```
After this change, you can upgrade to Loki Operator 0.12.

## What happens if you upgrade to 0.12 without migrating
If you upgrade to Loki Operator 0.12 while the LokiStack still references a BoltDB-based schema, Loki Operator will attempt reconciliation but will return an error and thus no changes will be made on the resources managed by the operator. The LokiStack will report a Degraded condition indicating that BoltDB-based schemas are no longer supported. This allows users to downgrade their Loki Operator version to perform this migration before they upgrade.

