module github.com/grafana/loki/cmd/index

go 1.24.0

toolchain go1.24.3

replace github.com/grafana/loki/v3 => ../..

require (
	github.com/alecthomas/kingpin/v2 v2.4.0
	github.com/dustin/go-humanize v1.0.1
	github.com/fatih/color v1.18.0
	github.com/grafana/loki/v3 v3.5.3
	github.com/prometheus/prometheus v0.305.1-0.20250806170547-208187eaa19b
)

require (
	github.com/alecthomas/units v0.0.0-20240927000941-0f3dac36c52b // indirect
	github.com/apache/arrow-go/v18 v18.4.0 // indirect
	github.com/axiomhq/hyperloglog v0.2.5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgryski/go-metro v0.0.0-20180109044635-280f6062b5bc // indirect
	github.com/efficientgo/core v1.0.0-rc.3 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc // indirect
	github.com/kamstrup/intmap v0.5.1 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.11 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.23.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.65.1-0.20250703115700-7f8b2a0d32d3 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/thanos-io/objstore v0.0.0-20250115091151-a54d0f04b42a // indirect
	github.com/xhit/go-str2duration/v2 v2.1.0 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/exp v0.0.0-20250506013437-ce4c2cf36ca6 // indirect
	golang.org/x/mod v0.26.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/protobuf v1.36.7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
