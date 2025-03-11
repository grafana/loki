---
title: Promtail pipeline stages
menuTitle:  Pipeline stages
description: Overview of the Promtail pipeline stages.
aliases: 
- ../../clients/promtail/stages/
weight:  700
---

# Promtail pipeline stages

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

This section is a collection of all stages Promtail supports in a
[Pipeline](../pipelines/).

Parsing stages:

  - [docker](docker/): Extract data by parsing the log line using the standard Docker format.
  - [cri](cri/): Extract data by parsing the log line using the standard CRI format.
  - [regex](regex/): Extract data using a regular expression.
  - [json](json/): Extract data by parsing the log line as JSON.
  - [logfmt](logfmt/): Extract data by parsing the log line as logfmt.
  - [replace](replace/): Replace data using a regular expression.
  - [multiline](multiline/): Merge multiple lines into a multiline block.
  - [geoip](geoip/): Extract geoip data from extracted labels.

Transform stages:

  - [template](template/): Use Go templates to modify extracted data.
  - [pack](pack/): Packs a log line in a JSON object allowing extracted values and labels to be placed inside the log line.
  - [decolorize](decolorize/): Strips ANSI color sequences from the log line.

Action stages:

  - [timestamp](timestamp/): Set the timestamp value for the log entry.
  - [output](output/): Set the log line text.
  - [labeldrop](labeldrop/): Drop label set for the log entry.
  - [labelallow](labelallow/): Allow label set for the log entry.
  - [labels](labels/): Update the label set for the log entry.
  - [limit](limit/): Limit the rate lines will be sent to Loki.
  - [sampling](sampling/): Sampling the lines will be sent to Loki.
  - [static_labels](static_labels/): Add static-labels to the log entry. 
  - [metrics](metrics/): Calculate metrics based on extracted data.
  - [tenant](tenant/): Set the tenant ID value to use for the log entry.
  - [structured_metadata](structured_metadata/): Add structured metadata to the log entry.

Filtering stages:

  - [match](match/): Conditionally run stages based on the label set.
  - [drop](drop/): Conditionally drop log lines based on several options.
