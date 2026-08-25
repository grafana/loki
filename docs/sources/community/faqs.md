---
title: Loki community frequently asked questions
menuTitle: Community FAQs
description: Answers some frequently asked questions about the Loki open source project.
weight: 50
keywords:
  - loki
  - open source
  - community
  - faq
---

# Loki community frequently asked questions

### Is Loki open source? What is the license?

Grafana Loki is open source, licensed under the GNU Affero General Public License v3 (AGPLv3). The license change from Apache 2.0 to AGPLv3 took effect in 2021.

The practical implication of AGPLv3 is that if you modify Loki and make it available over a network, you must make your modifications available under the same license. For most teams running Loki internally for log aggregation, this has no effect. If you are embedding Loki in a commercial product or offering it as a service, review the [license terms](https://github.com/grafana/loki/blob/main/LICENSE) or contact Grafana Labs about a commercial license.

Other components in the Grafana observability stack (Grafana, Tempo, Mimir, Alloy) each have their own license terms, detailed in their respective repositories.

### How is the documentation organized, and where do I find docs for a specific product?

The Loki documentation lives at [grafana.com/docs/loki/](https://grafana.com/docs/loki/latest/) and is organized into the following top-level sections:

- **[Get started](https://grafana.com/docs/loki/latest/get-started/)** — Architecture overview, deployment modes, and label concepts.
- **[Set up](https://grafana.com/docs/loki/latest/setup/)** — Installation instructions (Helm, Docker, local binary), migration guides, and upgrade steps.
- **[Configure](https://grafana.com/docs/loki/latest/configure/)** — Configuration reference and example configurations for common object storage backends.
- **[Send data](https://grafana.com/docs/loki/latest/send-data/)** — Guides for shipping logs to Loki using Grafana Alloy, Promtail, the Docker driver, and the HTTP API.
- **[Query](https://grafana.com/docs/loki/latest/query/)** — LogQL language reference, query examples, and the query API.
- **[Manage](https://grafana.com/docs/loki/latest/operations/)** — Day-two operations including storage, retention, caching, and multi-tenancy.
- **[Alert](https://grafana.com/docs/loki/latest/alert/)** — Recording rules and alerting rules powered by LogQL.

Documentation for other Grafana Labs products (Grafana, Mimir, Tempo, Alloy, and others) is available from the [Grafana documentation home page](https://grafana.com/docs/).

### Am I reading the right version of the docs?

The Loki documentation is versioned to match each release. By default, links from grafana.com and search results take you to the latest version, but if you arrived via an older link — for example, from a blog post, a forum thread, or a search result — you may be reading documentation for an older release.

To check which version you are reading, look at the version selector in the left-hand navigation panel. If it does not say **latest**, you can switch to the current version from that selector, or navigate directly using `/latest/` in the URL — for example, `grafana.com/docs/loki/latest/`.

If the version selector shows **next**, you are reading unpublished documentation for the upcoming release. This content may describe features or configuration options that are not yet available in any released version of Loki. The **next** docs are useful for previewing what is coming, but should not be used as a reference for your current deployment.

If a configuration option or feature described in the docs does not appear to exist in your Loki deployment, a version mismatch is the most common explanation. Check your installed Loki version (run `loki -version` or check the container image tag) and compare it against the version shown in the docs before assuming the documentation is wrong.

### How can I contribute to or report a problem with the docs?

The Loki documentation is open source and lives in the [`docs/sources`](https://github.com/grafana/loki/tree/main/docs/sources) directory of the Loki GitHub repository. Community contributions are welcome.

At the bottom of every documentation page you will find three options:

- **Suggest an edit** — opens the page source directly in GitHub so you can submit a pull request for a correction or improvement.
- **Contribute to docs** — for reporting a minor issue or mistake without making the edit yourself.
- **Report a problem** — for reporting technical issues with how the documentation page renders or behaves.

For questions that the documentation has not answered, the [Grafana Community Forum](https://community.grafana.com/) is the best place to ask. You can also join the `#loki` channel in the [Grafana Labs Community Slack](https://slack.grafana.com/) to discuss Loki-specific topics with maintainers and other users.

### How can I contribute to or report a problem with the code?

Loki is developed in the open on GitHub at [grafana/loki](https://github.com/grafana/loki). There are several ways to contribute:

- **Report a bug.** Open an [issue](https://github.com/grafana/loki/issues/new/choose) on GitHub. Include your Loki version, deployment mode, relevant configuration, and steps to reproduce the problem.
- **Suggest a feature.** Feature requests are also tracked as GitHub issues. Describe the use case and the behavior you would like to see.
- **Submit a pull request.** The full contribution guide is in [CONTRIBUTING.md](https://github.com/grafana/loki/blob/main/CONTRIBUTING.md). It covers building from source, running tests, code style, and the review process. The [Contributing to Loki](https://grafana.com/docs/loki/latest/community/contributing/) docs page provides a quick-start summary.
- **Propose a larger change.** For significant changes to Loki's architecture or behavior, write a [Loki Improvement Document (LID)](https://grafana.com/docs/loki/latest/community/lids/) and open it as a pull request for community discussion before starting implementation.

{{< admonition type="note" >}}
First-time contributors should look for issues labeled [`good first issue`](https://github.com/grafana/loki/labels/good%20first%20issue) for approachable starting points.
{{< /admonition >}}
