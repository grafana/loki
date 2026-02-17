.PHONY: generate bench list run run-debug stream grafana grafana-stop

# Default size for data generation (2GB)
SIZE ?= 2147483648
TENANT ?= test-tenant

generate:
	@echo "Generating benchmark data..."
	@go run ./cmd/generate/main.go -size $(SIZE) -tenant $(TENANT)

bench:
	@echo "Running all benchmarks..."
	@go test -bench=. -benchmem -count=1 -timeout=1h

list:
	@go run ./cmd/bench/main.go list

run:
	@go run ./cmd/bench/main.go run

run-debug:
	@DEBUG=1 go run ./cmd/bench/main.go run

stream:
	@go run ./cmd/stream/main.go | less

grafana:
	@echo "Starting Grafana with Loki datasource..."
	@docker run -d \
		--name loki-bench-grafana \
		-p 3000:3000 \
		-e GF_AUTH_ANONYMOUS_ENABLED=true \
		-e GF_AUTH_DISABLE_LOGIN_FORM=true \
		-e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
		-e GF_AUTH_BASIC_ENABLED=false \
		-e GF_USERS_DEFAULT_THEME=light \
		-e GF_CACHING_ENABLED=false \
		-e GF_PLUGINS_PREINSTALL_SYNC=grafana-lokiexplore-app@main@https://storage.googleapis.com/integration-artifacts/grafana-lokiexplore-app/release/main/any/grafana-lokiexplore-app-main.zip,grafana-lokioperational-app@main@https://storage.googleapis.com/integration-artifacts/grafana-lokioperational-app/release/latest/grafana-lokioperational-app-latest.zip \
		-v $(PWD)/grafana-datasource.yaml:/etc/grafana/provisioning/datasources/datasources.yml \
		-v $(PWD)/grafana-plugins.yaml:/etc/grafana/provisioning/plugins/app.yml \
		grafana/grafana:latest
	@echo "Grafana available at http://localhost:3000"
	@echo "Loki datasource pre-configured to connect to localhost:3100"

grafana-stop:
	@echo "Stopping Grafana container..."
	@docker stop loki-bench-grafana || true
	@docker rm loki-bench-grafana || true

loki-debug:
	@echo "Starting Loki on port 3100 with debug port 18001..."
	@(cd ../../../ && \
	make loki-debug && \
	dlv exec ./cmd/loki/loki-debug \
		--listen=:18001 \
		--headless=true \
		--api-version=2 \
		--accept-multiclient \
		--continue \
		-- -config.file=./pkg/logql/bench/config.yaml -config.expand-env=true -target=ui,dataobj-explorer,all)

loki:
	@echo "Starting Loki on port 3100 with debug port 18001..."
	@(cd ../../../ && \
		make loki && \
		cmd/loki/loki --config.file=./pkg/logql/bench/config.yaml -config.expand-env=true -target=ui,dataobj-explorer,all)

# server will start a local Loki instance and Grafana for interacting with bench data. You will need
# to manually kill the grafana process using `make grafana-stop` once you are done.
.PHONY: server
server: grafana loki

# server-debug will start a local Grafana and Loki with an exposed delve debug port, useful for
# interacting with bench data. The loki proceess will block, but can be killed using the DAP api,
# so this task defines a teardown command to kill grafana once loki is stopped.
.PHONY: server-debug
server-debug: grafana loki-debug

.PHONY: server-stop
server-stop:
	@make grafana-stop
	@pkill dlv
	@pkill loki
