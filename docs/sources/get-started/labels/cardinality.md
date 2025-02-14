---
title: Cardinality
description:  Describes what cardinality is and how it affects Loki performance.
weight: 
---

# Cardinality

The cardinality of a data attribute is the number of distinct values that the attribute can have.  For example, a boolean column in a database, which can only have a value of either `true` or `false` has a cardinality of 2.

High cardinality refers to a column or row in a database that can have many possible values. For an online shopping system, fields like `userId`, `shoppingCartId`, and `orderId` are often high-cardinality columns that can have hundreds of thousands of distinct values.

Other examples of high cardinality attributes include the following:

- Timestamp
- IP addresses
- Kubernetes pod names
- User ID
- Customer ID
- Trace ID

When we talk about _cardinality_ in Loki we are referring to the combination of labels and values and the number of log streams they create. In Loki, the fewer labels you use, the better. This is why Loki has a default limit of 15 index labels.

High cardinality can result from using labels with a large range of possible values, **or** combining many labels, even if they have a small and finite set of values, such as combining `status_code` and `action`. A typical set of status codes (200, 404, 500)  and actions (GET, POST, PUT, PATCH, DELETE) would create 15 unique streams. But, adding just one more label like `endpoint` (/cart, /products, /customers) would triple this to 45 unique streams.

To see an example of series labels and cardinality, refer to the [LogCLI tutorial](https://grafana.com/docs/loki/<LOKI_VERSION>/query/logcli/logcli-tutorial/#checking-series-cardinality).  As you can see, the cardinality for individual labels can be quite high, even before you begin combining labels for a particular log stream, which increases the cardinality even further.

To view the cardinality of your current labels, you can use [logcli](https://grafana.com/docs/loki/<LOKI_VERSION>/query/logcli/getting-started/).

`logcli series '{}' --since=1h --analyze-labels`

## Impact of high cardinality in Loki

High cardinality causes Loki to create many streams, especially when labels have many unique values, and when those values are short-lived (for example, active for seconds or minutes). This causes Loki to build a huge index, and to flush thousands of tiny chunks to the object store.

Loki was not designed or built to support high cardinality label values. In fact, it was built for exactly the opposite. It was built for very long-lived streams and very low cardinality in the labels. In Loki, the fewer labels you use, the better.  

High cardinality can lead to significant performance degradation.

## Avoiding high cardinality

To avoid high cardinality in Loki, you should:

- Avoid assigning labels with unbounded values, for example timestamp, trace ID, order ID.
- Prefer static labels that describe the origin or context of the log message, for example, application, namespace, environment.
- Don't assign "dynamic" labels, which are values from the log message itself, unless it is low-cardinality, or a long-lived value.
- Use structured metadata to store frequently-searched, high-cardinality metadata fields, such as customer IDs or transaction IDs, without impacting Loki's index.

{{< admonition type="note" >}}
[Structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) is a feature in Loki and Cloud Logs that allows customers to store metadata that is too high cardinality for log lines, without needing to embed that information in log lines themselves.  
It is a great home for metadata which is not easily embeddable in a log line, but is too high cardinality to be used effectively as a label. [Query acceleration with Blooms](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/bloom-filters/) also utilizes structured metadata.
{{< /admonition >}}
