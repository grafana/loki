.PHONY: generate bench list run run-debug stream grafana grafana-stop discover remote-test generate-k6 gotestsum-remote build-correctness-metrics

# Load local secrets from .env (ignored by git).
# Copy .env.template → .env and fill in real values.
-include .env
export

# Default size for data generation (2GB)
SIZE ?= 2147483648
TENANT ?= test-tenant

# Discover configuration — override via .env or environment
DISCOVER_MAX_STREAMS ?= 500
DISCOVER_CONCURRENCY ?= 5

# gotestsum and correctness-metrics configuration
GOTESTSUM ?= $(shell which gotestsum 2>/dev/null)
RANGE_TYPE ?= range
METADATA_DIR ?= testdata

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

############
# Discover #
############

# Build the discover binary
discover/cmd/discover:
	CGO_ENABLED=0 go build -o $@ ./$(@D)

# discover runs the metadata discovery pipeline against the live Loki cluster.
# Requires secrets in .env (copy .env.template and fill in values):
#   LOKI_ADDR, LOKI_USERNAME, LOKI_PASSWORD     — Loki endpoint and auth
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY    — S3 credentials for TSDB index access
#   S3_BUCKET, S3_REGION, S3_ENDPOINT           — S3 bucket configuration
#   TABLE_PREFIX                                — TSDB index table name prefix
discover: discover/cmd/discover
ifndef LOKI_ADDR
	$(error LOKI_ADDR is required — add it to .env (see .env.template))
endif
ifndef LOKI_USERNAME
	$(error LOKI_USERNAME is required — add it to .env (see .env.template))
endif
ifndef LOKI_PASSWORD
	$(error LOKI_PASSWORD is required — add it to .env (see .env.template))
endif
ifndef AWS_ACCESS_KEY_ID
	$(error AWS_ACCESS_KEY_ID is required — add it to .env (see .env.template))
endif
ifndef AWS_SECRET_ACCESS_KEY
	$(error AWS_SECRET_ACCESS_KEY is required — add it to .env (see .env.template))
endif
ifndef S3_BUCKET
	$(error S3_BUCKET is required — add it to .env (see .env.template))
endif
ifndef S3_REGION
	$(error S3_REGION is required — add it to .env (see .env.template))
endif
ifndef S3_ENDPOINT
	$(error S3_ENDPOINT is required — add it to .env (see .env.template))
endif
ifndef TABLE_PREFIX
	$(error TABLE_PREFIX is required — add it to .env (see .env.template))
endif
	./discover/cmd/discover \
		--tenant $(LOKI_USERNAME) \
		--storage-type s3 \
		--s3.buckets "$(S3_BUCKET)" \
		--s3.region "$(S3_REGION)" \
		--s3.endpoint "$(S3_ENDPOINT)" \
		--s3.access-key-id "$(AWS_ACCESS_KEY_ID)" \
		--s3.secret-access-key "$(AWS_SECRET_ACCESS_KEY)" \
		--table-prefix "$(TABLE_PREFIX)" \
		--selector '$(DISCOVER_SELECTOR)' \
		--max-streams $(DISCOVER_MAX_STREAMS) \
		--concurrency $(DISCOVER_CONCURRENCY) \
		--output testdata \
		--queries-dir queries \
		$(if $(DISCOVER_FROM),--from "$(DISCOVER_FROM)") \
		$(if $(DISCOVER_TO),--to "$(DISCOVER_TO)")

###########################
# Remote Correctness Test #
###########################

# Default timeout for remote correctness tests
REMOTE_TIMEOUT ?= 30m

# remote-test compares query results between two live Loki endpoints.
# Requires:
#   1. Discovered metadata in testdata/ (run `make discover` first)
#   2. Secrets in .env (see .env.template):
#      REMOTE_ADDR_1, REMOTE_ADDR_2  — two Loki endpoints to compare
#      REMOTE_ORG_ID                 — tenant org ID
#      LOKI_USERNAME, LOKI_PASSWORD  — HTTP basic auth
remote-test:
ifndef REMOTE_ADDR_1
	$(error REMOTE_ADDR_1 is required — add it to .env (see .env.template))
endif
ifndef REMOTE_ADDR_2
	$(error REMOTE_ADDR_2 is required — add it to .env (see .env.template))
endif
ifndef REMOTE_ORG_ID
	$(error REMOTE_ORG_ID is required — add it to .env (see .env.template))
endif
	go test -tags=remote_correctness -v \
		./pkg/logql/bench \
		-run TestRemoteStorageEquality \
		-timeout $(REMOTE_TIMEOUT) \
		-addr-1 "$(REMOTE_ADDR_1)" \
		-addr-2 "$(REMOTE_ADDR_2)" \
		-org-id "$(REMOTE_ORG_ID)" \
		-username "$(LOKI_USERNAME)" \
		-password "$(LOKI_PASSWORD)" \
		-metadata-dir testdata

#############################
# k6 Test Case Generation   #
#############################

TENANT_ID ?= 29

# generate-k6 resolves query registry templates against discovered metadata
# and outputs JSON files for the k6 Goldfish comparison test suite.
# Requires: discovered metadata in testdata/ (run `make discover` first)
generate-k6:
	go run ./cmd/generate-k6/main.go \
		--metadata-dir testdata \
		--queries-dir queries \
		--tenant-id $(TENANT_ID) \
		--output-dir testdata

#####################################
# Correctness Metrics Binary        #
#####################################

build-correctness-metrics:
	@mkdir -p ./pkg/logql/bench/build
	CGO_ENABLED=0 go build -o ./pkg/logql/bench/build/correctness-metrics ./pkg/logql/bench/cmd/correctness-metrics

#####################################
# gotestsum Remote Test Runner      #
#####################################

# gotestsum-remote runs the remote correctness tests via gotestsum,
# producing JUnit XML output in ./build/results.xml.
# Requires gotestsum in $PATH: go install gotest.tools/gotestsum@latest
gotestsum-remote:
ifndef GOTESTSUM
	$(error gotestsum not found in PATH — install with: go install gotest.tools/gotestsum@latest)
endif
ifndef REMOTE_ADDR_1
	$(error REMOTE_ADDR_1 is required — add it to .env (see .env.template))
endif
ifndef REMOTE_ADDR_2
	$(error REMOTE_ADDR_2 is required — add it to .env (see .env.template))
endif
ifndef REMOTE_ORG_ID
	$(error REMOTE_ORG_ID is required — add it to .env (see .env.template))
endif
	@mkdir -p ./pkg/logql/bench/build
	$(GOTESTSUM) \
		--format standard-verbose \
		--junitfile ./pkg/logql/bench/build/results.xml \
		-- -tags=remote_correctness -v \
		./pkg/logql/bench \
		-run TestRemoteStorageEquality \
		-timeout $(REMOTE_TIMEOUT) \
		-addr-1 "$(REMOTE_ADDR_1)" \
		-addr-2 "$(REMOTE_ADDR_2)" \
		-org-id "$(REMOTE_ORG_ID)" \
		-username "$(LOKI_USERNAME)" \
		-password "$(LOKI_PASSWORD)" \
		-metadata-dir $(METADATA_DIR) \
		-remote-range-type $(RANGE_TYPE)
