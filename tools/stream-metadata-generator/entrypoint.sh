#!/bin/sh

# Create required directories
mkdir -p /tmp/loki/chunks /tmp/loki/rules

# Set proper permissions
chown -R nobody:nobody /tmp/loki
chmod -R 777 /tmp/loki

# Start Loki
exec loki --config.file=/etc/loki/local-config.yaml 