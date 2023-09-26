---
title: Prometheus pipeline stages
menuTitle:  Pipeline stages
description: Overview of the Promtail pipeline stages.
aliases: 
- ../../clients/promtail/stages/
weight:  700
---

# Prometheus pipeline stages

This section is a collection of all stages Promtail supports in a
[Pipeline]({{< relref "../pipelines" >}}).

Parsing stages:

  - [docker]({{< relref "./docker" >}}): Extract data by parsing the log line using the standard Docker format.
  - [cri]({{< relref "./cri" >}}): Extract data by parsing the log line using the standard CRI format.
  - [regex]({{< relref "./regex" >}}): Extract data using a regular expression.
  - [json]({{< relref "./json" >}}): Extract data by parsing the log line as JSON.
  - [logfmt]({{< relref "./logfmt" >}}): Extract data by parsing the log line as logfmt.
  - [replace]({{< relref "./replace" >}}): Replace data using a regular expression.
  - [multiline]({{< relref "./multiline" >}}): Merge multiple lines into a multiline block.
  - [geoip]({{< relref "./geoip" >}}): Extract geoip data from extracted labels.

Transform stages:

  - [template]({{< relref "./template" >}}): Use Go templates to modify extracted data.
  - [pack]({{< relref "./pack" >}}): Packs a log line in a JSON object allowing extracted values and labels to be placed inside the log line.
  - [decolorize]({{< relref "./decolorize" >}}): Strips ANSI color sequences from the log line.

Action stages:

  - [timestamp]({{< relref "./timestamp" >}}): Set the timestamp value for the log entry.
  - [output]({{< relref "./output" >}}): Set the log line text.
  - [labeldrop]({{< relref "./labeldrop" >}}): Drop label set for the log entry.
  - [labelallow]({{< relref "./labelallow" >}}): Allow label set for the log entry.
  - [labels]({{< relref "./labels" >}}): Update the label set for the log entry.
  - [limit]({{< relref "./limit" >}}): Limit the rate lines will be sent to Loki.
  - [sampling]({{< relref "./sampling" >}}): Sampling the lines will be sent to Loki.
  - [static_labels]({{< relref "./static_labels" >}}): Add static-labels to the log entry. 
  - [metrics]({{< relref "./metrics" >}}): Calculate metrics based on extracted data.
  - [tenant]({{< relref "./tenant" >}}): Set the tenant ID value to use for the log entry.
  - [structured_metadata]({{< relref "./structured_metadata" >}}): Add structured metadata to the log entry.

Filtering stages:

  - [match]({{< relref "./match" >}}): Conditionally run stages based on the label set.
  - [drop]({{< relref "./drop" >}}): Conditionally drop log lines based on several options.
