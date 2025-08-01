name: Loki Helm Chart Feature Request
description: Suggest an idea for the Loki Helm chart
title: "[loki] Helm Feature Request Title"
labels: [enhancement, area/helm]
assignees:
  - 
body:
  - type: markdown
    attributes:
      value: |
        Thanks for taking the time to fill out this feature request for the Loki Helm chart!
        
        For general Loki usage questions, please use the [Grafana Community Slack](https://slack.grafana.com/). This issue tracker is for feature requests specific to the Loki Helm chart.

  - type: input
    id: chart-name
    attributes:
      label: Which chart?
      description: Enter the name of the chart for this feature request (e.g., loki, loki-stack, loki-distributed). Note we only `officially` support the `loki` chart.
      placeholder: loki
    validations:
      required: true

  - type: dropdown
    id: deployment-mode
    attributes:
      label: What's your Loki deployment mode?
      description: Select the deployment mode this feature request relates to
      options:
        - Single Binary (monolithic)
        - Simple Scalable (read, write, backend components)
        - Microservices (all components separated)
        - All deployment modes
        - Not applicable
    validations:
      required: true

  - type: textarea
    id: desc
    attributes:
      label: Is your feature request related to a problem ?
      description: Give a clear and concise description of what the problem is.
      placeholder:  ex. I'm always frustrated when [...]
    validations:
      required: true

  - type: textarea
    id: prop-solution
    attributes:
      label: Describe the solution you'd like.
      description: A clear and concise description of what you want to happen.
    validations:
      required: true
      
  - type: textarea
    id: alternatives
    attributes:
      label: Describe alternatives you've considered.
      description: A clear and concise description of any alternative solutions or features you've considered. If nothing, please enter `NONE`
    validations:
      required: true

  - type: textarea
    id: additional-ctxt
    attributes:
      label: Additional context.
      description: |
        Add any other context about the feature request here, such as:
        - Use case details or user stories
        - Configuration examples or desired values.yaml changes
        - Related Loki features or components
        - Links to relevant documentation or discussions
        - Screenshots or mockups if applicable
    validations:
      required: false