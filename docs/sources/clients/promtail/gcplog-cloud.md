---
title: Cloud setup GCP Logs
---
# Cloud setup GCP logs

This document explain how one can setup Google Cloud Platform to forward its cloud resource logs from a particular GCP project into Google Pubsub topic so that is available for Loki promtail to consume.

This document assumes, that reader have `gcloud` installed and have required permissions(as mentioned in #[Roles and Permission] section)

## Roles and Permission

User should have following roles to complete the setup.
- "roles/pubsub.editor"
- "roles/logging.configWriter"

## Setup Pubsub Topic

Google Pubsub Topic will act as the queue to persist log messages which then can be read from `promtail`.

```bash
$ gcloud pubsub topics create $TOPIC_ID
```

e.g:
```bash
$ gcloud pubsub topics create cloud-logs
```

## Setup Log Router

We create a log sink to forward cloud logs into pubsub topic created before

```bash
$ gcloud logging sinks create $SINK_NAME $SINK_LOCATION $OPTIONAL_FLAGS
```

e.g:
```bash
$ gcloud logging sinks create cloud-logs pubsub.googleapis.com/projects/my-project/topics/cloud-logs \
--log-filter='resource.type=("gcs_bucket")' \
--description="Cloud logs"
```

Above command also adds `log-filter` option which represents what type of logs should get into the destination `pubsub` topic.
For more information on adding `log-filter` refer this [document](https://cloud.google.com/logging/docs/export/configure_export_v2#creating_sink)

We cover more advanced `log-filter` [below](#Advanced-Log-filter)

## Create Pubsub subscription for Loki

We create subscription for the pubsub topic we create above and `promtail` uses this subscription to consume log messages.

```bash
$ gcloud pubsub subscriptions create cloud-logs --topic=$TOPIC_ID \
--ack-deadline=$ACK_DEADLINE \
--message-retention-duration=$RETENTION_DURATION \
```

e.g:
```bash
$ gcloud pubsub subscriptions create cloud-logs --topic=pubsub.googleapis.com/projects/my-project/topics/cloud-logs \
--ack-deadline=10s \
--message-retention-duration=7d \
```

For more fine grained options, refer to the `gcloud pubsub subscriptions --help`

## ServiceAccount for Promtail

We need a service account with following permissions.
- pubsub.subscriber

This enables promtail to read log entries from the pubsub subscription created before.

you can find example for promtail scrape config for `gcplog` [here](../scraping/#gcplog-scraping)

If you are scraping logs from multiple GCP projects, then this serviceaccount should have above permissions in all the projects you are tyring to scrape.

## Operations

Sometimes you may wish to clear the pending pubsub queue containing logs.

These messages stays in Pubsub Subscription until they're acknowledged. The following command removes log messages without needing to be consumed via promtail or any other pubsub consumer.

```bash
gcloud pubsub subscriptions seek <subscription-path> --time=<yyyy-mm-ddThh:mm:ss>
```

To delete all the old messages until now, set `--time` to current time.

```bash
gcloud pubsub subscriptions seek projects/my-project/subscriptions/cloud-logs --time=$(date +%Y-%m-%dT%H:%M:%S)
```

## Advanced log filter

So far we've covered admitting GCS bucket logs into Loki, but often one may need to add multiple cloud resource logs and may also need to exclude unnecessary logs. The following is a more complex example.

We use the `log-filter` option to include logs and the `exclusion` option to exclude them.

### Use Case
Include following cloud resource logs
- GCS bucket
- Kubernetes
- IAM
- HTTP Load balancer

And we exclude specific HTTP load balancer logs based on payload and status code.

```
$ gcloud logging sinks create cloud-logs pubsub.googleapis.com/projects/my-project/topics/cloud-logs \
--log-filter='resource.type=("gcs_bucket OR k8s_cluster OR service_account OR iam_role OR api OR audited_resource OR http_load_balancer")' \
--description="Cloud logs" \
--exclusion='name=http_load_balancer,filter=<<EOF
resource.type="http_load_balancer"
(
	(
		jsonPayload.statusDetails=("byte_range_caching" OR "websocket_closed")
	)
		OR
	(
		http_request.status=(101 OR 206)
	)
)
EOF
```
