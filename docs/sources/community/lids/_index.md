---
title: Loki Improvement Documents (LIDs)
description: Loki Improvement Documents (LIDs)
aliases: 
- ../lids/
- ../community/lids/
weight: 500
---

# Loki Improvement Documents (LIDs)

## Purpose

Loki Improvement Documents (_LIDs_) are proposals for modifying Grafana Loki's feature-set and/or processes. These documents serve to promote engagement between the community and maintainers _before_ making large changes to Grafana Loki. This ensures that we will only work on features that the maintainers and the community _actually want_, implemented in line with Loki's engineering and scalability considerations.

LIDs are **not** required for:

- bugfixes
- minor features
- minor process changes

## Creating a LID

Start by opening a PR against this repository, using [this template](https://github.com/grafana/loki/blob/main/docs/sources/lids/template.md).

All LIDs require a "sponsor". A sponsor is a Grafana Loki maintainer who is willing to shepherd the improvement proposal throughout its development process from draft through to completion. A sponsor can be found by starting a thread in our [mailing list](https://groups.google.com/forum/#!forum/lokiproject), which one or more maintainers will respond to and volunteer. If a LID is generated internally by a Grafana Labs employee who is also a maintainer, the sponsor will be the author. Thread topics should be prefixed with "LID: ".

LIDs should contain a high-level overview of the problem, the proposed solution, and other details specified in the template. LIDs can optionally have a _rough prototype_ implementation PR associated with it, but it is advised to not spend too much time on it because the proposal may be rejected.

LIDs will be viewable in perpetuity, and serve to document our decisions plus all the inputs and reasoning that went into those decisions.

## Process

Once a PR is submitted, it will be reviewed by the sponsor, as well as interested community members and maintainers. LIDs require approval from the sponsor and one additional maintainer to be accepted. Once accepted, work can commence on the improvement and the nominated sponsor(s) will review all further related contributions.

## Notes

- LIDs will be assigned a number once accepted.
- LIDs must be kept up-to-date by the sponsor and/or author after the initial PR (which adds the LID) is merged, to reflect its current state.
- A LID is considered completed once it is either rejected or the improvement has been included in a release.
- `CHANGELOG` entries should reference LIDs where applicable.
- Significant changes to the LID process should be proposed [with a LID](https://www.google.com/search?q=recursion).
- LIDs should be shared with the community on the [`#loki` channel on Slack](https://slack.grafana.com) for comment, and the sponsor should wait **at least 2 weeks** before accepting a proposal.
