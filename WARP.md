# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project summary
Gostats collects Dell PowerScale OneFS statistics via the OneFS API (PAPI) from one or more clusters and forwards them to a pluggable backend. Supported backends include InfluxDB (v1 and v2), Prometheus (per-cluster `/metrics` HTTP endpoint), and a discard/no-op backend.

## Toolchain
- Go version: `go.mod` specifies Go `1.24.6` and CI uses Go `1.24` (see `.github/workflows/go.yml`).

## Common commands (PowerShell)
All commands are intended to run from the repo root.

### Build
```pwsh
# Direct
go build .

# Makefile wrapper (if you have `make` installed on Windows)
make build
```

### Run locally
```pwsh
# Create a local config (default path is ./idic.toml)
Copy-Item .\example_isi_data_insights_d.toml .\idic.toml

# Run
go run . -config-file .\idic.toml

# Or run the built binary
.\gostats.exe -config-file .\idic.toml
```

Useful flags (see `main.go`):
- `-config-file` (default `idic.toml`)
- `-logfile`, `-loglevel`
- `-version`
- `-check-stat-return` (debugging: verifies API returns all requested stats)

### Tests
```pwsh
# All tests
go test -v ./...

# Makefile wrapper
make test

# Run a single test by name
go test -run TestDecodeStat_ArrayOfMaps -v .

# Run a subset by regex
go test -run '^TestDecodeStat_' -v .
```

### Formatting / basic lint
There is no dedicated linter configuration checked into this repo.
```pwsh
# Format
gofmt -w .

# Basic static checks
go vet ./...
```

## Configuration notes
- The config file is TOML; see `tomlConfig` in `config.go` and the example `example_isi_data_insights_d.toml`.
- Backend selection is controlled by `global.stats_processor` (plugin names are matched in `main.go:getDBWriter()`):
  - `discard`
  - `influxdb`
  - `influxdbv2`
  - `prometheus`
- Secret interpolation: password/token fields may be specified as `$env:VARNAME`. At runtime, `config.go:secretFromEnv()` replaces that value with the corresponding environment variable.
- Prometheus mode:
  - Each `[[cluster]]` needs `prometheus_port` when `global.stats_processor = "prometheus"`.
  - Optional Prometheus HTTP service discovery: when `prom_http_sd.enabled = true` and Prometheus backend is selected, the collector also serves a small JSON endpoint listing scrape targets (see `prometheus.go:startPromSdListener`).

## Code architecture (big picture)

### Runtime flow
- `main.go`
  - Parses flags, loads config (`config.go:mustReadConfig`), configures logging (`logging.go`).
  - Parses stat groups and validates active groups (`parseStatConfig`).
  - Starts Prometheus HTTP SD listener (only when Prometheus backend + `prom_http_sd.enabled`).
  - Spawns one goroutine per enabled cluster, each running `statsloop()`.

- `main.go:statsloop()` (per cluster)
  - Creates a `Cluster` (`isilon_api.go`) and connects/authenticates (session cookie with CSRF support, or basic auth).
  - Fetches per-stat metadata from `/platform/1/statistics/keys/<KEY>` to determine update intervals (`Cluster.fetchStatDetails`).
  - Buckets stats into collection intervals (`calcBuckets`) and schedules work using a min-heap priority queue (`pq.go`).
  - Collects stats from `/platform/1/statistics/current` (requests are chunked to avoid overly long URLs; see `MaxAPIPathLen` in `isilon_api.go`).
  - Optionally collects summary stats from `/platform/3/statistics/summary/*`.
  - Converts API returns into backend-neutral `Point` objects and writes them via the configured backend.

### Data model and decoding
- `isilon_api.go` defines `StatResult` where `Value` can be primitive, map, array, or nested structures depending on the statistic.
- `backend.go` contains the core decoding pipeline:
  - `DecodeStat()` / `decodeValue()` flatten nested JSON into:
    - `[]ptFields` (field-name -> value)
    - `[]ptTags` (tag-name -> tag-value)
  - These are combined into a `Point` (measurement name + timestamp + aligned field/tag arrays).

### Backend/plugin interface
- `statssink.go` defines `DBWriter`:
  - `Init(clusterName string, config *tomlConfig, ci int, sd map[string]statDetail) error`
  - `WritePoints(points []Point) error`
- Implementations:
  - `influxdb.go`: InfluxDB v1 batch writes.
  - `influxdbv2.go`: InfluxDB v2 async writes.
  - `prometheus.go`: maintains an in-memory sample store and exposes it via a per-cluster HTTP server; samples expire based on OneFS update interval.
  - `discard.go`: no-op writer.

### Networking details for Prometheus
- Prometheus listeners use `net.ListenConfig` with a `Control` function to set socket options (SO_REUSEADDR and, on non-Windows, SO_REUSEPORT).
  - Windows implementation: `control_windows.go`
  - Non-Windows implementation: `control_unix.go`

## Adding a new backend
- Implement `DBWriter` in a new `*.go` file.
- Register it in `main.go:getDBWriter()` (plugin name string -> constructor).
- The backend receives `[]Point` where multi-valued stats are represented as multiple field/tag entries under the same measurement name and timestamp (the arrays are aligned by index).