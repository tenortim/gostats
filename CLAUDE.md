# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go build              # Build the binary
go build -v ./...     # Build with verbose output (used in CI)
go test -v ./...      # Run all tests
make build            # Alias for go build
make test             # Alias for go test -v ./...
```

Run a single test:
```bash
go test -v -run TestName ./...
```

Requires Go 1.24+.

## Architecture

Gostats is a statistics collection daemon that polls Dell PowerScale OneFS clusters via the OneFS Platform API (PAPI) and writes metrics to a pluggable backend.

### Core Flow

```
main() → parse config → setup logging → validate stat groups
       → per-cluster goroutine (one per cluster):
           → connect to OneFS API (session or basic auth)
           → fetch stat metadata
           → initialize backend (DBWriter)
           → event loop driven by priority queue:
               → collect stats at scheduled intervals
               → encode API responses to Point structs
               → write batched Points to backend
               → reschedule collection
```

### Key Components

- **`main.go`** — Entry point. Spawns one goroutine per cluster. Manages the priority queue scheduling loop. Uses a `WaitGroup` for shutdown.
- **`isilon_api.go`** — OneFS HTTP API client. Handles session/basic auth, SSL verification, retry logic with exponential backoff, and tracking of unavailable stats.
- **`backend.go`** — Converts raw API JSON responses into `Point` structs (with fields and tags). Recursively flattens nested stats. Filters known-broken stats.
- **`statssink.go`** — Defines the `DBWriter` interface (`Init()` + `WritePoints()`) and the `Point` data structure.
- **`config.go`** — Parses TOML config (supports versions 0.31 and 0.32). Supports `$env:VARNAME` substitution for secrets.
- **`pq.go`** — Min-heap priority queue used to schedule stat collections at varying intervals.
- **`logging.go`** — Structured logging via `slog`. Supports text/JSON output to file and/or stdout. Custom levels: TRACE, DEBUG, INFO, NOTICE, WARN, ERROR, CRITICAL, FATAL.

### Backend Implementations

Selected via `stats_processor` in config. All implement `DBWriter`:

| File | Backend | Config key |
|------|---------|------------|
| `influxdb.go` | InfluxDB v1 | `"influxdb"` |
| `influxdbv2.go` | InfluxDB v2 | `"influxdbv2"` |
| `prometheus.go` | Prometheus (per-cluster HTTP endpoint) | `"prometheus"` |
| `discard.go` | No-op (testing) | `"discard"` |

### Configuration

The config file is `idic.toml` by default (see `example_isi_data_insights_d.toml`). Key sections:

- `[global]` — `version` (required), `stats_processor`, `active_stat_groups`, `min_update_interval_override`
- `[logging]` — log file path, format (`text`/`json`), stdout toggle
- `[influxdb]` / `[influxdbv2]` / `[prometheus]` — backend-specific connection settings
- `[[cluster]]` — one stanza per cluster; supports per-cluster `prometheus_port` and `preserve_case` override
- `[[statgroup]]` — groups of stat keys with `update_interval` (`"*"` = use stat's native interval, or integer seconds)
- `[summary_stats]` — enables protocol/client summary stat collection (disabled by default)

Secrets can be injected via environment variables: `password = "$env:MY_SECRET"`.

### Platform-Specific Files

- `control_unix.go` / `control_windows.go` — socket control options (`SO_REUSEADDR`, etc.) for Prometheus HTTP listeners. Use build tags to select platform.
