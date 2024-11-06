module github.com/grafana/loki/v3/pkg/logql/log

go 1.23.1

require (
	github.com/Masterminds/sprig/v3 v3.3.0
	github.com/dustin/go-humanize v1.0.1
	github.com/grafana/jsonparser v0.0.0-20241004153430-023329977675
	github.com/grafana/loki/v3/pkg/logql/log/jsonexpr v0.0.0
	github.com/grafana/loki/v3/pkg/logql/log/logfmt v0.0.0
	github.com/grafana/loki/v3/pkg/logql/log/pattern v0.0.0
	github.com/grafana/loki/v3/pkg/util/regexp v0.0.0
	github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc
	github.com/json-iterator/go v1.1.12
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.60.1
	github.com/prometheus/prometheus v0.55.1
	github.com/stretchr/testify v1.9.0
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba
)

require (
	dario.cat/mergo v1.0.1 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spf13/cast v1.7.0 // indirect
	go.opentelemetry.io/collector/pdata v1.19.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.28.0 // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/grpc v1.67.1 // indirect
	google.golang.org/protobuf v1.35.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/grafana/loki/v3/pkg/logql/log/jsonexpr => ./jsonexpr
	github.com/grafana/loki/v3/pkg/logql/log/logfmt => ./logfmt
	github.com/grafana/loki/v3/pkg/logql/log/pattern => ./pattern
	github.com/grafana/loki/v3/pkg/util/regexp => ../../util/regexp
)
