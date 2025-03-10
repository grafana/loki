---
title: "0001: Introducing LIDs"
description: "0001: Introducing LIDs"
aliases: 
- ../../lids/0001-introduction/
---

# 0001: Introducing LIDs

**Author:** Danny Kopping (danny.kopping@grafana.com)

**Date:** 01/2023

**Sponsor(s):** @dannykopping

**Type:** Process

**Status:** Accepted

**Related issues/PRs:** N/A

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):** N/A

---

## Background

As the Grafana Loki project grows, we have seen more and more contributions from external (outside Grafana Labs) contributors.

## Problem Statement

Many of these external contributions are large and complex, and have taken these contributors significant time to implement. Large contributions that are made without prior discussion with maintainers are at risk of being rejected if they are misguided, implemented inefficiently, or simply undesired; this is obviously suboptimal both for the contributors and the maintainers.

Aside from external contributions, changes being proposed by Grafana Loki maintainers may also require community engagement before being worked on.

## Goals

It would be preferable to engage with contributors _before_ they make large contributions to ensure that both their and the project's interests are aligned. The community at large must also have a voice when feature or process changes are being proposed, to protect their own interests.

We should implement a **lightweight** process that guides the implementation of major changes to the project.

## Proposals

### Proposal 0: Do nothing

We will continue to attract large, often complex, external contributions that have not be discussed with maintainers prior to the work being put in; this may lead to suboptimal outcomes for the relationship between the project and its community.

### Proposal 1: Loki Improvement Documents

Inspired by Python's [PEP](https://peps.python.org/pep-0001/) and Kafka's [KIP](https://cwiki.apache.org/confluence/display/KAFKA/Kafka+Improvement+Proposals) approaches, we should create a process for formally documenting improvements to Loki which are permanently viewable, and document our decisions.

## Other Notes

Google Docs were considered for this, but they are less useful because:
- they would need to be owned by the Grafana Labs organisation, so that they remain viewable even if the author closes their account
- we already have previous [design documents](../../design-documents/) in our documentation and, in a recent ([5th Jan 2023](https://docs.google.com/document/d/1MNjiHQxwFukm2J4NJRWyRgRIiK7VpokYyATzJ5ce-O8/edit#heading=h.78vexgrrtw5a)) community call, the community expressed a preference for this type of approach
