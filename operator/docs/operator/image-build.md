---
title: "Operator Image Build Process"
description: "Documentation on how the Loki Operator images are built and pushed"
lead: ""
date: 2024-01-01T09:00:00+00:00
lastmod: 2024-01-01T09:00:00+00:00
draft: false
images: []
menu:
  docs:
    parent: "operator"
weight: 100
toc: true
---

This document describes the Loki Operator image build and push strategy.

## Build & Push Strategy

### DockerHub (docker.io)

| Image | Trigger | Organization | Tag | Workflow File |
|-------|---------|-------------|-----|---------------|
| `loki-operator` | Push to main | `grafana` | `latest` | `operator-images.yaml` |
| `loki-operator` | Release created | `grafana` | `{version}` | `operator-release-please.yml` |

### Quay.io

| Image | Trigger | Organization | Tag | Workflow File |
|-------|---------|-------------|-----|---------------|
| `loki-operator` | Push to main | `openshift-logging` | `latest` | `operator-images.yaml` |
| `loki-operator-bundle` | Push to main | `openshift-logging` | `latest` | `operator-images.yaml` |
| `storage-size-calculator` | Push to main | `openshift-logging` | `latest` | `operator-images.yaml` |

## Workflow Files

### `.github/workflows/operator-images.yaml`

- **Triggers**: Push to main, pull requests, manual dispatch
- **Purpose**: Continuous integration builds
- **Images**: All operator images for development and testing

### `.github/workflows/operator-release-please.yml`

- **Triggers**: When release-please creates a new release
- **Purpose**: Production release builds
- **Images**: Versioned release images

### `.github/workflows/operator-reusable-image-build.yml`

- **Purpose**: Centralized build workflow used by all other workflows
- **Features**: Multi-platform builds (amd64, arm64, arm), registry-based authentication

## Image Details

| Image | Dockerfile | Context | Purpose |
|-------|------------|---------|---------|
| `loki-operator` | `operator/Dockerfile` | `operator/` | Main operator binary |
| `loki-operator-bundle` | `operator/bundle/openshift/bundle.Dockerfile` | `operator/bundle/openshift/` | OpenShift bundle |
| `storage-size-calculator` | `operator/calculator.Dockerfile` | `operator/` | Storage calculator utility |
