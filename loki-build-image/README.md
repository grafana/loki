# Build image

## Versions

### 0.34.4

- Update to Go 1.23.5

### 0.34.0

- Update to Go 1.23.1
- Update to Alpine 3.20.3

### 0.33.6

- Update to go 1.22.6

### 0.33.5

- Update to alpine 3.20.2

### 0.33.4

- Update to go 1.22.5

### 0.33.2

- Update to go 1.22.2

### 0.33.1-golangci.1.51.2

- Update to Go version 1.21.9 but restore golangci-lint to v1.51.2

* This release should only be used for the release branches such as 2.9.x and 2.8.x. *

### 0.33.1

- Update to Go 1.21.9

### 0.33.0

- Update to Alpine 3.18.5

### 0.30.1

- Update to Go version 1.21.3

### 0.30.0

- Update to Go version 1.21.2
- Update to Alpine version 3.18.4

### 0.29.3-golangci.1.51.2

- Update to Go version 1.20.6 but restore golangci-lint to v1.51.2

* This release should only be used for the release branches such as 2.8.x and 2.7.x. *
The current release of the build image uses golangci-lint to v1.53.2 which makes
a lot of linter checks mandatory requiring a huge amount of fixes.
See https://github.com/grafana/loki/pull/9601. To avoid the integration problems this
build image will be used in those branches.

### 0.29.3

- Update to Go version 1.20.6
