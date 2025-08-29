# Loki Helm Chart Development

## Chart Structure

The Loki Helm chart is located in `production/helm/loki/` and follows standard Helm conventions.

```
production/helm/loki/
├── templates/           # Helm template files
├── unittests/          # Helm unit tests (mirrors templates/ structure)  
├── values.yaml         # Default chart values
├── Chart.yaml          # Chart metadata
└── Makefile           # Chart-specific commands
```

## Testing

### Helm Unit Tests

Run helm unit tests using Docker:

```bash
make helm-unittest     # run all helm unit tests
```

Unit tests are located in `unittests/` and use the [helm-unittest](https://github.com/helm-unittest/helm-unittest) plugin. The directory structure mirrors the `templates/` directory. Tests use Docker to ensure consistency across environments.