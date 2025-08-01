name: Loki Helm Chart Bug Report
description: Create a report to help us improve
title: "[loki] Helm Issue Request Title"
labels: [bug, area/helm]
assignees:
  - 
body:
  - type: markdown
    attributes:
      value: |
        Thanks for taking the time to fill out this bug report for the Loki Helm chart! Please be cautious with the sensitive information/logs while filing the issue.
        
        For general Loki usage questions, please use the [Grafana Community Slack](https://slack.grafana.com/) and join the `#loki` channel. This issue tracker is for bugs specific to the Loki Helm chart.
  - type: textarea
    id: desc
    attributes:
      label: Describe the bug a clear and concise description of what the bug is.
    validations:
      required: true

  - type: input
    id: helm-version
    attributes:
      label: What's your helm version?
      description: Enter the output of `$ helm version`
      placeholder: Copy paste the entire output of the above 
    validations:
      required: true
  - type: input
    id: kubectl-version
    attributes:
      label: What's your kubectl version?
      description: Enter the output of `$ kubectl version`
    validations:
      required: true

  - type: input
    id: chart-name
    attributes:
      label: Which chart?
      description: Enter the name of the chart where you encountered this bug (e.g., loki, loki-stack, loki-distributed). Note we only `officially` support the `loki` chart.
      placeholder: loki
    validations:
      required: true

  - type: input
    id: chart-version
    attributes:
      label: What's the chart version?
      description: Enter the version of the chart that you encountered this bug.
    validations:
      required: true

  - type: dropdown
    id: deployment-mode
    attributes:
      label: What's your Loki deployment mode?
      description: Select the deployment mode you're using
      options:
        - Single Binary (monolithic)
        - Simple Scalable (read, write, backend components)
        - Distributed (all components separated)
    validations:
      required: true

  - type: textarea
    id: what-happened
    attributes:
      label: What happened?
      description: Enter exactly what happened.
    validations:
      required: false

  - type: textarea
    id: what-expected
    attributes:
      label: What you expected to happen?
      description: Enter what you expected to happen.
    validations:
      required: false

  - type: textarea
    id: how-to-reproduce
    attributes:
      label: How to reproduce it?
      description: As minimally and precisely as possible.
    validations:
      required: false

  - type: textarea
    id: changed-values
    attributes:
      label: Enter the changed values of values.yaml?
      description: Please enter only values which differ from the defaults. Enter `NONE` if nothing's changed. Include any Loki configuration, storage, authentication, or resource settings you've modified.
      placeholder: |
        loki:
          auth_enabled: true
        minio:
          enabled: true
        gateway:
          enabled: true
    validations:
      required: false

  - type: textarea
    id: helm-command
    attributes:
      label: Enter the command that you execute and failing/misfunctioning.
      description: Enter the command as-is as how you executed.
      placeholder: helm install my-loki grafana/loki --version 6.34.0 --values values.yaml
    validations:
      required: true

  - type: textarea
    id: anything-else
    attributes:
      label: Anything else we need to know?
      description: |
        Include any relevant information such as:
        - Kubernetes cluster details (version, provider)
        - Storage backend being used (filesystem, S3, GCS, etc.)
        - Log volume or specific error messages
        - Related components (Grafana, Promtail, etc.)
    validations:
      required: false