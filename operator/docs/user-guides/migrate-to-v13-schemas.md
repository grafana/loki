# Migrating out of the Deprecated BoltDB-Based Storage Schema

Loki Operator 0.12 will ship a version of Loki that no longer supports BoltDB-based schemas. As a result any LokiStack instance that still uses a BoltDB-based schema (v11 or v12) must complete the migration described below before upgrading to Loki Operator 0.12. If a LokiStack still references a BoltDB-based schema after upgrading, Loki Operator 0.12 will produce an error during reconciliation.

Before starting, update your Loki Operator to the latest supported patch version for your current selected release.

## Migration Process

### Step 1: Detect your current state

Run the following command to inspect the schemas on your LokiStack:

```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.storage.schemas'
```

You should be in one of these three situations:

| Situation | Command result | Action required |
|-----------|----------------|-----------------|
| Single TSDB schema | Only schema is `version: v13` | Go to Step 2a |
| Already on TSDB but with older schemas | Latest entry is `version: v13` | Go to Step 2b |
| Still haven't migrated to TSDB | Latest entry is either `version: v11` or `version: v12` | Go to Step 2c |

### Step 2: Determine your current state and act

Based on your current state, follow only the sub-step that applies to you.

#### Step 2a: Single TSDB schema

If your LokiStack only has a single v13 schema entry then you are fully migrated and you can proceed to upgrade to Loki Operator 0.12, no further steps required.

#### Step 2b: Already on TSDB but with older schemas

If your LokiStack already has a v13 schema entry but still references older schemas, proceed to Step 3 to determine how long to wait before upgrading to Loki Operator 0.12.

#### Step 2c: Still haven't migrated to TSDB

If your LokiStack is either on v11 or v12 schema, append a new v13 entry with a date in the future while keeping the existing entries. For example using the CLI:

```
oc patch lokistack <lokistack-name> -n <lokistack-namespace> --type json -p '[
  {
    "op": "add",
    "path": "/spec/storage/schemas/-",
    "value": {
      "version": "v13",
      "effectiveDate": "'"$(date -u -d '+2 days' +%Y-%m-%d)"'"
    }
  }
]'
```

Note: `effectiveDate` takes effect at 00:00 UTC on the specified date. The Loki Operator requires this time to be more than two hours in the future. To avoid validation errors near midnight UTC, we recommend choosing a date two days ahead of the current UTC date.

After this change, proceed to Step 3.

### Step 3: Retention and wait time

From the v13 `effectiveDate`, Loki writes new data using the v13 (TSDB) schema. Data ingested before that date stays on the previous schema.

Loki Operator 0.12 cannot query schemas older than v13. Before you upgrade, every log still inside your retention window must have been written with v13.

By default, LokiStack does not limit log retention, Logs accumulate indefinitely and never age out. This means you cannot safely assume a fixed wait time unless retention is actually enforced. Before computing how long to wait, make sure retention is properly configured.

#### Step 3.1: Check your current configuration

- Check for a bucket lifecycle policy on your object storage bucket (via your cloud provider's console or CLI).
- Check the LokiStack retention setting:

```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o jsonpath='{.spec.limits.global.retention.days}'
```

#### Step 3.2: Ensure a retention is configured

Configuring retention or bucket expiry permanently deletes logs that have fallen out of the retention window. Confirm your organization's retention requirements before proceeding.

To complete the migration, your bucket lifecycle policy and LokiStack retention must both be set. In particular the LokiStack retention should be configured to be at least 3 days longer than the bucket lifecycle expiry. This keeps the bucket lifecycle policy as the primary enforcement mechanism while LokiStack retention acts purely as a fallback. The 3-day margin absorbs the delay most cloud providers have between an object's expiry and the lifecycle rule actually deleting it.

If your current configuration already satisfies this (a lifecycle policy exists, and LokiStack retention set and is at least 3 days longer), skip to Step 3.3.

Otherwise, configure it now:

1. Set a bucket lifecycle policy on your object storage bucket with an expiry of your choice (pick 30 days if you have no existing requirement as that's the maximum retention supported). If you already have a lifecycle policy, keep its current expiry, let's call this value N.
2. Set LokiStack retention to N + 3 days:

```
oc patch lokistack <lokistack-name> -n <lokistack-namespace> --type merge -p '{"spec":{"limits":{"global":{"retention":{"days": <N+3>}}}}}'
```

#### Step 3.3: Compute the wait time

The wait starts from the v13 `effectiveDate`. Once retention is configured, your wait time is simply the LokiStack global retention value:

```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '[.spec.limits | .. | objects | .retention? // empty | .days, .streams[]?.days] | max // 0'
```

Note: The above command checks all the retention fields and returns the maximum wait time. If you do not have `jq` make sure to check:
1. Global default retention: `.spec.limits.global.retention.days`
2. Tenant default retention: `.spec.limits.tenants.<tenant-name>.retention.days`, for every tenant.
3. Stream retention rules: `.spec.limits.global.retention.streams[].days` and `.spec.limits.tenants.<tenant-name>.retention.streams[].days`, for every rule.

If a tenant/stream override is shorter than the global value, that doesn't shorten your wait, global retention still governs the rest of your data. If a tenant/stream override is longer than the global retention, use that value instead.

When the applicable number of days have passed since your v13 `effectiveDate`, you can proceed to Step 4.

### Step 4: Verify the v13 schema is active and queryable

After the wait from Step 3, confirm v13 is active before removing old entries.

Check that the LokiStack has accepted the new schema and is `Ready`:

```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.status.conditions[] | select(.type=="Ready") | .status'
```

Verify that the recent logs are queryable:

```
TOKEN=$(oc whoami -t)
GATEWAY=$(oc get route <lokistack-name> -n <lokistack-namespace> -o jsonpath='{.spec.host}')
curl -k -H "Authorization: Bearer ${TOKEN}" "https://${GATEWAY}/api/logs/v1/infrastructure/loki/api/v1/query_range" --data-urlencode 'query={kubernetes_namespace_name="<lokistack-namespace>"}' --data-urlencode 'limit=5'| jq
```

Proceed to Step 5 only after both checks pass.

### Step 5: Removing the old schemas

Note: it's not possible to complete this step unless you've completed Step 3.

Loki Operator 0.12 will not reconcile LokiStacks that reference v11 or v12, so those entries must be removed before you upgrade.

The LokiStack spec must still contain the v13 schema with its original `effectiveDate`. Do not add a new v13 entry.

```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json \
  | jq '.spec.storage.schemas |= map(select(.version == "v13"))' \
  | oc apply -f -
```

After patching, confirm that only v13 remains and that the LokiStack condition is `Ready`:

```
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.spec.storage.schemas'
oc get lokistack <lokistack-name> -n <lokistack-namespace> -o json | jq '.status.conditions[] | select(.type=="Ready") | .status'
```

After this change, you can upgrade to Loki Operator 0.12.

## What happens if you upgrade to 0.12 without migrating

Upgrading to Loki Operator 0.12 while the LokiStack still references a BoltDB schema, will result in Loki Operator returning an error and thus no changes will be made on the resources managed by the operator. The LokiStack will report a `Degraded` condition indicating that BoltDB-based schemas are no longer supported. This allows users to downgrade their Loki Operator version to perform this migration before they upgrade.
