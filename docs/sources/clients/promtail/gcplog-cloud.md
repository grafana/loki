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
$ gcloud beta logging sinks create $SINK_NAME $SINK_LOCATION $OPTIONAL_FLAGS
```

e.g:
```bash
$ gcloud beta logging sinks create cloud-logs pubsub.googleapis.com/projects/my-project/topics/cloud-logs \
--log-filter='resource.type=("gcs_bucket")' \
--description="Cloud logs"
```

Above command also adds `log-filter` option which represents what type of logs should get into the destination `pubsub` topic.
For more information on adding `log-filter` refer this [document](https://cloud.google.com/logging/docs/export/configure_export_v2#creating_sink)

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
