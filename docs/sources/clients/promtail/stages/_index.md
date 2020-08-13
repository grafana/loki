---
title: Stages
---
# Stages

This section is a collection of all stages Promtail supports in a
[Pipeline](../pipelines/).

Parsing stages:

  * [docker](docker/): Extract data by parsing the log line using the standard Docker format.
  * [cri](cri/): Extract data by parsing the log line using the standard CRI format.
  * [regex](regex/): Extract data using a regular expression.
  * [json](json/): Extract data by parsing the log line as JSON.
  * [replace](replace/): Replace data using a regular expression.

Transform stages:

  * [template](template/): Use Go templates to modify extracted data.

Action stages:

  * [timestamp](timestamp/): Set the timestamp value for the log entry.
  * [output](output/): Set the log line text.
  * [labels](labels/): Update the label set for the log entry.
  * [metrics](metrics/): Calculate metrics based on extracted data.
  * [tenant](tenant/): Set the tenant ID value to use for the log entry.

Filtering stages:

  * [match](match/): Conditionally run stages based on the label set.
  * [drop](drop/): Conditionally drop log lines based on several options.

