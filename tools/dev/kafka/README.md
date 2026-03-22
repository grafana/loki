# Loki Kafka Development Setup

This directory contains the development environment for testing Loki with Kafka integration. The setup provides supporting services (Kafka, Grafana, and a log generator) via docker-compose, while Loki itself needs to be run manually from source code for development purposes.

## Quick Start

1. Start the supporting services (Kafka, Grafana, Log Generator):
   ```bash
   docker-compose up -d
   ```

2. Run Loki manually with Kafka configuration:
   ```bash
   # From the root of the Loki repository
   go run ./cmd/loki/main.go --config.file=tools/dev/kafka/loki-local-config.debug.yaml --log.level=debug -target=all
   ```

   Note: Loki is not included in docker-compose as it's intended to be run directly from source code for development.

## Services

### Kafka
- Broker accessible at `localhost:9092`
- Uses KRaft (no ZooKeeper required)
- Single broker setup for development
- Topic `loki` is used for log ingestion

### Kafka UI
- Web interface available at http://localhost:8080
- Monitor topics, messages, and consumer groups
- No authentication required

### Grafana
- Available at http://localhost:3000
- Anonymous access enabled (Admin privileges)
- Pre-configured with Loki data source
- Features enabled:
  - Loki logs dataplane
  - Explore logs shard splitting
  - Loki explore app

### Log Generator
- Automatically sends sample logs to Loki
- Useful for testing and development
- Configured to push logs directly to Loki's HTTP endpoint

## Configuration Files

- `docker-compose.yaml`: Service definitions and configuration
- `loki-local-config.debug.yaml`: Loki configuration with Kafka enabled
  - Kafka ingestion enabled
  - Local storage in `/tmp/loki`
  - Debug logging enabled

## Debugging

### VSCode Configuration
Create `.vscode/launch.json` in the root directory:
```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch Loki (Kafka)",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/cmd/loki/main.go",
            "args": [
                "--config.file=../../tools/dev/kafka/loki-local-config.debug.yaml",
                "--log.level=debug",
                "-target=all"
            ],
            "buildFlags": "-mod vendor"
        }
    ]
}
```

## Common Tasks

### View Logs
```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f kafka
docker-compose logs -f grafana
```

### Reset Environment
```bash
# Stop and remove containers
docker-compose down

# Remove local Loki data
rm -rf /tmp/loki

# Start fresh
docker-compose up -d
```

### Verify Setup
1. Check Kafka UI (http://localhost:8080) for:
   - Broker health
   - Topic creation
   - Message flow

2. Check Grafana (http://localhost:3000):
   - Navigate to Explore
   - Select Loki data source
   - Query logs using LogQL

## Troubleshooting

- **Loki fails to start**: Check if port 3100 is available
- **No logs in Grafana**:
  - Verify Kafka topics are created
  - Check log generator is running
  - Verify Loki is receiving data through Kafka UI
- **Kafka connection issues**:
  - Ensure broker is running (`docker-compose ps`)
  - Check broker logs (`docker-compose logs broker`)
