PKG := github.com/goccy/go-json

BIN_DIR := $(CURDIR)/bin
PKGS := $(shell go list ./... | grep -v internal/cmd|grep -v test)
COVER_PKGS := $(foreach pkg,$(PKGS),$(subst $(PKG),.,$(pkg)))

COMMA := ,
EMPTY :=
SPACE := $(EMPTY) $(EMPTY)
COVERPKG_OPT := $(subst $(SPACE),$(COMMA),$(COVER_PKGS))

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

.PHONY: cover
cover:
	go test -coverpkg=$(COVERPKG_OPT) -coverprofile=cover.out ./...

.PHONY: cover-html
cover-html: cover
	go tool cover -html=cover.out

.PHONY: lint
lint: golangci-lint
	$(BIN_DIR)/golangci-lint run

golangci-lint: | $(BIN_DIR)
	@{ \
		set -e; \
		GOLANGCI_LINT_TMP_DIR=$$(mktemp -d); \
		cd $$GOLANGCI_LINT_TMP_DIR; \
		go mod init tmp; \
		GOBIN=$(BIN_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2; \
		rm -rf $$GOLANGCI_LINT_TMP_DIR; \
	}

.PHONY: generate
generate:
	go generate ./internal/...

# Fails if the benchmarks of go-json under ./benchmarks are degraded compared with the master branch:
# their mean is slower beyond -tolerance, or one of them is slower beyond -single-tolerance.
# The benchmarks of the other libraries are never measured.
# Options of internal/cmd/benchcheck can be passed by BENCH_CHECK_FLAGS.
# e.g.) make bench-check BENCH_CHECK_FLAGS="-bench GoJson -no-cache"
.PHONY: bench-check
bench-check:
	go run ./internal/cmd/benchcheck $(BENCH_CHECK_FLAGS)

# bench-calibrate measures the benchmarks of HEAD against themselves, linked with other function layouts,
# and prints how much the results of identical code differ: the noise which bench-check has to see beyond.
# e.g.) make bench-calibrate BENCH_CHECK_FLAGS="-group decode"
.PHONY: bench-calibrate
bench-calibrate:
	go run ./internal/cmd/benchcheck -calibrate -attempts 1 $(BENCH_CHECK_FLAGS)

# bench-compare-encode prints the encode benchmarks of go-json and of bytedance/sonic side by side.
# SonicStd is sonic configured to do what encoding/json, and go-json, do ( escape HTML, sort the keys of a map ),
# and GoJsonLikeSonic is go-json configured to do what sonic does by default.
# SonicFastest is sonic.ConfigFastest. SONIC_MAX_INLINE_DEPTH sets how deep sonic inlines the nested structs.
# The Twitter benchmarks are the ones of sonic itself, with go-json beside it ( benchmarks/sonic_bench_test.go, under the license of sonic ).
.PHONY: bench-compare-encode
bench-compare-encode:
	cd benchmarks && go test -run '^$$' -bench '^Benchmark_(Encode|Marshal|EncodeBigData|MarshalBigData).*_(GoJson|GoJsonLikeSonic|Sonic|SonicFastest|SonicStd)$$' -benchtime 300ms -count 3 .
	cd benchmarks && go test -run '^$$' -bench '^Benchmark_Twitter' -benchtime 300ms -count 3 .

# bench-compare-decode prints the decode benchmarks of go-json and of bytedance/sonic side by side.
# Sonic is sonic.ConfigDefault and SonicStd is sonic.ConfigStd, which validates the strings as encoding/json does.
# SonicFastest is sonic at its fastest, and GoJsonUnmarshalOfNoCopyString go-json at its ( benchmarks/sonic_decode_test.go ).
# SonicFastestValidating is SonicFastest with the checks which go-json does: the values which are skipped and the
# strings are validated.
# The Twitter benchmarks decode the payload of sonic's own benchmarks ( benchmarks/sonic_bench_test.go ), and the
# Keys ones structs whose keys differ only, in length or in script ( benchmarks/decode_key_length_test.go ), and the
# GitHub ones the responses of the REST and the GraphQL APIs of GitHub ( benchmarks/decode_github_test.go ), and
# the OpenAI and Anthropic ones the responses and the streams of the APIs of LLMs ( benchmarks/llm_api_test.go ).
# BENCH_LIVE_HEAP_MB gives the benchmarks a live heap of that size, as a real program has, which sets the goal of the
# GC for every library alike ( benchmarks/live_heap_test.go ).
.PHONY: bench-compare-decode
bench-compare-decode:
	cd benchmarks && go test -run '^$$' -bench '^Benchmark_Decode_(Small|Medium|Large)Struct_Unmarshal_(GoJson|GoJsonUnmarshalOfNoCopyString|Sonic|SonicStd|SonicFastest|SonicFastestValidating)$$' -benchtime 300ms -count 3 .
	cd benchmarks && go test -run '^$$' -bench '^Benchmark_Decode_Twitter' -benchtime 300ms -count 3 .
	cd benchmarks && go test -run '^$$' -bench '^Benchmark_Decode_(Short|Medium|Long|NonASCII|UnknownNonASCII)Keys_Unmarshal_' -benchtime 300ms -count 3 .
	cd benchmarks && go test -run '^$$' -bench '^Benchmark_Decode_GitHub(REST|GraphQL)_Unmarshal_' -benchtime 300ms -count 3 .
	cd benchmarks && go test -run '^$$' -bench '^Benchmark_Decode_(OpenAIChatCompletion|OpenAIResponse|AnthropicMessage)(Stream)?_Unmarshal_' -benchtime 300ms -count 3 .

# bench-report measures the JSON libraries doing the same work, as the benchmark report does on the CI, and
# renders the page and the summary into benchmarks/site ( see benchmarks/report_test.go ).
.PHONY: bench-report
bench-report:
	mkdir -p benchmarks/site
	cd benchmarks && BENCH_REPORT_OUT=$(CURDIR)/benchmarks/site/results-$$(go env GOARCH).json go test -run '^TestReport$$' -count=1 -timeout 60m -test.benchtime=100ms .
	cd benchmarks && go run ./cmd/benchreport -out site site/results-$$(go env GOARCH).json

# bench-profile-decode prints where the CPU time of the decode benchmarks of go-json goes.
.PHONY: bench-profile-decode
bench-profile-decode:
	cd benchmarks && for b in Decode_SmallStruct_Unmarshal_GoJson Decode_MediumStruct_Unmarshal_GoJson Decode_LargeStruct_Unmarshal_GoJson Decode_TwitterBinding_Unmarshal_GoJson Decode_TwitterGeneric_Unmarshal_GoJson Decode_LargeStruct_Stream_GoJson; do \
		go test -run '^$$' -bench "^Benchmark_$$b$$" -benchtime 3s -cpuprofile /tmp/$$b.prof -o /tmp/bench.test . > /dev/null && \
		echo "=== $$b" && go tool pprof -top -nodecount=22 /tmp/bench.test /tmp/$$b.prof 2>/dev/null | tail -n +5 && \
		echo "--- by line" && go tool pprof -top -lines -nodecount=30 /tmp/bench.test /tmp/$$b.prof 2>/dev/null | tail -n +6; \
	done

# bench-profile-encode prints where the CPU time of the encode benchmarks of go-json goes.
.PHONY: bench-profile-encode
bench-profile-encode:
	cd benchmarks && for b in Encode_SmallStructCached_GoJson Encode_MediumStructCached_GoJson Encode_LargeStructCached_GoJson Encode_MapInterface_GoJson TwitterGeneric_GoJson TwitterGeneric_GoJsonLikeSonicFast TwitterParallelBinding_GoJson; do \
		go test -run '^$$' -bench "^Benchmark_$${b}$$" -benchtime 3s -cpuprofile /tmp/$$b.prof -o /tmp/bench.test . > /dev/null && \
		echo "=== $$b" && go tool pprof -top -nodecount=22 /tmp/bench.test /tmp/$$b.prof 2>/dev/null | tail -n +5 && \
		echo "--- by line" && go tool pprof -top -lines -nodecount=30 /tmp/bench.test /tmp/$$b.prof 2>/dev/null | tail -n +6; \
	done

# bench-variants measures the candidates of the optimizations against what is used now.
.PHONY: bench-variants
bench-variants:
	go test -run '^$$' -bench 'BenchmarkVariant|BenchmarkAppendString|BenchmarkScanString' -benchtime 300ms -count 2 ./internal/encoder/
