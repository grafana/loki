---
title: "DRAFT: Add Support for Oracle OCI Object Storage"
description: "Add Support for Oracle OCI Object Storage, including OCI IAM instance principal authentication"
---

# DRAFT: Add Support for Oracle OCI Object Storage

**Author:** Eric R. Rath (eric.rath@oracle.com)

**Date:** 02/2023

**Sponsor(s):** @username of maintainer(s) willing to shepherd this LID

**Type:** Feature

**Status:** Draft

**Related issues/PRs:**

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):**

---

## Background

Loki supports storing data on a local filesystem, and in a variety of object stores.  The [Loki docs](https://grafana.com/docs/loki/v2.7.x/operations/storage/filesystem/) clarify that the filesystem option is weaker than object stores on scaling, durability, and high availability.

## Problem Statement

Loki supports several object stores, but not Oracle's OCI Object Storage.  While Oracle OCI Object Storage includes an S3-compatibility API, this still requires the use of credentials, even when running on an OCI compute instance.  This allows Loki to store objects in OCI Object Storage, but requires key credentials.  

It's nice to avoid distributing long-lasting keys when possible, and Oracle OCI supports [instance principal authentication](https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/callingservicesfrominstances.htm) when running on an OCI compute instance.  This allows calling services from OCI compute instances.  The calls are still authenticated, but the authentication is handled automatically, access is still controlled by OCI IAM policies, and credentials are not required.

But this is outside the scope of the S3 SDK, and thus not available when using Loki's S3 client.

## Goals

Add an Oracle OCI Object Storage chunk client that supports both key-based authentication and instance principal authentication.

## Non-Goals (optional)

This proposal does not aim to change any of the general client behavior, or chunk logic.  This proposal also does not aim to add an index client, but instead rely on the "single-store" approach that requires only a chunk client.

## Proposals

### Proposal 0: Do nothing

People running Loki on OCI compute instances that _want_ to put their data into OCI Object Storage will continue to use Loki's S3 client, and key-based authentication.

**Pros**:
- Nothing to be done

**Cons**:
- People running Loki on OCI compute instances that want to put data into OCI Object Storage have to use Loki's S3 client, and supply key credentials.

### Proposal 1: Title

Add an Oracle OCI Object Storage chunk client.  This will allow people running Loki on an OCI compute instance to use _either_ key-based authentication _or_ instance principal authentication.

**Pros**:
- People running Loki on OCI compute instances can put data into OCI Object Storage without having to supply key credentials.

**Cons**:
- One more client to maintain?

## Other Notes

None at this time.
