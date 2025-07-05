---
title: Grafana Log Dashboard Guide
menuTitle: Grafana Log Dashboard Guide
description: Step-by-step guide for setting up Grafana, Loki, and Promtail for log aggregation and visualization.
weight: 310
---

---

This guide walks you through the process of setting up a Grafana Server Log Monitoring Dashboard, including the installation and configuration of Grafana, Loki, and Promtail for log aggregation and visualization. It also covers setting up scripts for automating log retrieval and dashboard updates.

---

## Step 1: Install Grafana via CLI

1. **Install Grafana** on your server using the following instructions for Debian-based systems:
   - Follow the installation guide here: [Grafana Installation on Debian](https://grafana.com/docs/grafana/latest/setup-grafana/installation/debian/)

2. Once installed, start Grafana and make sure it's accessible via a port (default: `3000`).

3. After Grafana is running, create an admin account. Once logged in, you'll land on the Grafana homepage.

---

## Step 2: Install Loki

1. **Install Loki** by following the official instructions for Docker:
   - [Loki Installation via Docker](https://grafana.com/docs/loki/latest/setup/install/docker/)

---

## Step 3: Create a Docker Compose File for Loki

To set up Loki with Grafana, create a `docker-compose.yml` file:

```yaml
version: "3"  # The version can be removed if you're using a recent Docker Compose version.

services:
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    networks:
      - loki
    volumes:
      - grafana-data:/path # [CHANGE THIS LINE] Adding persistence for Grafana data 

  loki:
    image: grafana/loki:latest
    ports:
      - "3100:3100"
    networks:
      - loki
    volumes:
      - ./path  # [CHANGE THIS LINE] Local directory for Loki data
    user: "10001"  # Run Loki as the appropriate user to avoid permission issues

  promtail:
    image: grafana/promtail:latest
    command: "-config.file=/etc/promtail/config.yaml" # [CHANGE THIS LINE] 
    networks:
      - loki
    volumes:
      - "/var/log:/var/log"  # [CHANGE THIS LINE] Mounting host logs

networks:
  loki:

volumes:
  grafana-data:  # Declaring the volume for Grafana
```

---

## Step 4: Install Promtail

1. **Install Promtail** by following the official instructions:
   - [Promtail Installation Guide](https://grafana.com/docs/loki/latest/send-data/promtail/installation/)

---

## Step 5: Configure Data Sources (Logs)

To aggregate logs from other servers, we set up a method of transferring logs to the Grafana server. The logs are pulled into the Grafana server, where they are processed and rendered on a Grafana dashboard.

Here’s the updated section of the `README` with the requested changes marked. The rest remains unchanged.

---

## Step 5: Configure Data Sources (Logs)

### 5.1: Write a Bash Script to Pull Logs

Here’s an example of a script (`transfer_logs.sh`) to pull logs from another server:

```bash
#!/bin/bash

# Define variables
REMOTE_SERVER="root@server_ip"  # [CHANGE THIS LINE] Remote server IP
SSH_KEY_PATH="path"  # [CHANGE THIS LINE] Path to SSH key
DEST_DIR="path"  # [CHANGE THIS LINE] Destination directory on the Grafana server

# Define paths for logs
DOCKER_LOG="path/to/docker/log"  # [CHANGE THIS LINE] Path to Docker log on remote server
NGINX_LOG="path/to/nginx/log"  # [CHANGE THIS LINE] Path to NGINX log on remote server
SYSLOG="path/to/syslog"  # [CHANGE THIS LINE] Path to syslog on remote server

# Create directories if they don't exist
mkdir -p "$DEST_DIR/docker" "$DEST_DIR/nginx" "$DEST_DIR/syslog"

# Transfer logs
echo "Transferring Docker log..."
ssh -i $SSH_KEY_PATH $REMOTE_SERVER "cat $DOCKER_LOG" > "$DEST_DIR/docker/docker.log"

echo "Transferring NGINX log..."
ssh -i $SSH_KEY_PATH $REMOTE_SERVER "cat $NGINX_LOG" > "$DEST_DIR/nginx/error.log"

echo "Transferring syslog..."
ssh -i $SSH_KEY_PATH $REMOTE_SERVER "cat $SYSLOG" > "$DEST_DIR/syslog/syslog.log"

echo "All logs have been successfully transferred."
```

---

### 5.2: Configure Promtail to Scrape Logs

Next, create a `promtail-config.yaml` file to let Promtail know where the logs are located:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://server_ip:3100/loki/api/v1/push  # [CHANGE THIS LINE] Loki's URL

scrape_configs:
  - job_name: docker_logs
    static_configs:
      labels:
        job: docker
        __path__: /path/to/logs/on/grafana/server/docker/*.log  # [CHANGE THIS LINE] Path to Docker logs
  - job_name: nginx_logs
    static_configs:
      labels:
        job: nginx
        __path__: /path/to/logs/on/grafana/server/nginx/*.log  # [CHANGE THIS LINE] Path to NGINX logs
  - job_name: syslog_logs
    static_configs:
      labels:
        job: syslog
        __path__: /path/to/logs/on/grafana/server/syslog/*.log  # [CHANGE THIS LINE] Path to syslog logs
```

---

## Step 6: Configure Grafana to Use Loki

1. Open Grafana’s homepage and click the **three vertical lines** in the top-left corner.

2. Navigate to **Data Sources**, then click **Add Data Source**.

3. Select **Loki** as the data source type.

4. Define the **URL** of your Loki instance, which is typically `http://localhost:3100`.

5. Click **Save & Test**. You should see a success message confirming the connection.

---

## Step 7: Explore Data in Grafana

1. Go to the **Explore** section in Grafana.

2. Use label filters and run queries to see the logs coming from Promtail. The labels in the filters correspond to the `labels` defined in the `promtail-config.yaml`.

---

## Step 8: Create a Dashboard

1. On the left sidebar in Grafana, click **Dashboards** > **New Dashboard**.

2. Click **Add Panel** to create a new panel.

3. Under **Data Source**, select **Loki**.

4. Write queries to retrieve logs for display. Use different labels to filter log data as needed.

5. Choose a visualization type (e.g., **Logs**).

6. Repeat the process to add more visualizations, then adjust the layout and size to suit your needs.

7. Save the dashboard once you are satisfied with the layout.

---

## Step 9: Automate Log Retrieval and Update Dashboard

To make the process of pulling logs and updating the Grafana dashboard more automated, create two Python scripts:

### 9.1: `transfer_server.py`

This script triggers the Promtail process to start log scraping when accessed via an HTTP request:

```python
from http.server import BaseHTTPRequestHandler, HTTPServer
import subprocess

COMMAND = "sudo ./promtail-linux-amd64 -config.file=promtail-config.yaml"

class RequestHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/run":
            try:
                subprocess.Popen(COMMAND, shell=True)
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"Command executed successfully.")
            except Exception as e:
                self.send_response(500)
                self.end_headers()
                self.wfile.write(f"Error executing command: {e}".encode())
        else:
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b"Not found.")

if __name__ == "__main__":
    server_address = ('server_ip', 8081)
    httpd = HTTPServer(server_address, RequestHandler)
    print("Server running on http://server_ip. Access /run to execute the command.")
    httpd.serve_forever()
```

### 9.2: `server.py`

This script pulls the logs by executing the previously created bash script:

```python
from http.server import BaseHTTPRequestHandler, HTTPServer
import subprocess

class ScriptHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        script_path = "/path/to/transfer_logs.sh" # [CHANGE THIS LINE] 

        try:
            subprocess.run([script_path], check=True)
            self.send_response(200)
            self.send_header("Content-type", "text/html")
            self.end_headers()
            self.wfile.write(b"Logs Pulled Successfully.")
        except subprocess.CalledProcessError:
            self.send_response(500)
            self.send_header("Content-type", "text/html")
            self.end_headers()
            self.wfile.write(b"Failed to pull logs.")

def run(server_class=HTTPServer, handler_class=ScriptHandler, port=8080):
    server_address = ('', port)
    httpd = server_class(server_address, handler_class)
    print(f"Starting http server on port {port}")
    httpd.serve_forever()

if __name__ == "__main__":
    run()
```

Run both Python scripts in the background using `nohup`:

```bash
nohup python3 transfer_server.py > transfer_server.log 2>&1 &
nohup python3 server.py > server.log 2>&1 &
```

---

## Step 10: Add Buttons to Grafana Dashboard

1. In Grafana, go to **Settings** > **Links**.

2. Add the links to the above Python scripts with the appropriate port (`8080` for log pulling and `8081` for triggering Promtail).

3. This will enable users to pull logs instantly as they wish. Make sure promtail activation button is run first when opening dashboard, and then pulling logs button after.