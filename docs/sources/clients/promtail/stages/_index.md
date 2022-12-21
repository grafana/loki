---
title: Stages
---
# Stages

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
  - [static_labels](static_labels/): Add static-labels to the log entry. 
  - [metrics](metrics/): Calculate metrics based on extracted data.
  - [tenant](tenant/): Set the tenant ID value to use for the log entry.

Filtering stages:

  - [match](match/): Conditionally run stages based on the label set.
  - [drop](drop/): Conditionally drop log lines based on several options.
