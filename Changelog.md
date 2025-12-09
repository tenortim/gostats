<!-- markdownlint-disable MD024 -->
# Changelog

## 0.31 Tue Dec 9 12:28:46 2025 -0800

> [!IMPORTANT]
> The following changes mean that a minimum version of Golang 1.24 is required to build.
>
> The config rewrite is a breaking change which will require
> a manual update of the configuration file post-upgrade.

- Switch to log/slog for logging
  - Numerous reasons including log/slog being part of the standard library, supporting structured logging and easily allowing multiple log levels for diferent components.

## 0.30 Wed Nov 19 11:23:36 2025 -0800

### Bug Fixes

- Fix handling of degraded stats
  - When a node in the cluster is down, stat returns are marked degraded. The stat engine will still retrieve data but the stat returns are marked with an error code. Unfortunately, code to handle missing stats and add them to a list of "bad" stats that should not be collected also caught the degraded case and so these stats ended up "disappearing".
  - Added a full list of the expected return types and handle them all appropriately.
  - All stats now have a "degraded" label/tag that is set to true or false depending on whether we received a degraded result.
  - Fix node info argument to current stats endpoint
  - The argument is "show_nodes=true", not "node_info=true".

## 0.29 Wed Nov 12 14:01:19 2025 -0800

> [!IMPORTANT]
> The lnn code change below does not work because it passed the wrong argument to the endpoint. Use version 0.30 above.

### Changes

- Fix regular stats node vs devid
  - The general stats endpoint returns a devid which is less usable. It takes an argument that also returns the logical node number. Add code to grab the lnn
- Update/improve code comments

### Bug Fixes

- Fix Windows build after reuseaddr change
  - The interface to set SO_REUSEADDR on the socket is different on Windows than on Unix. Refector to pull out the listenconfig control routine into two files, one for Windows, one for the rest.

## v0.28 - Fri Sep 19 10:29:10 2025 -0700

### Changes

- Rewrite stat decoder, add many tests
  - The stat decoder is now a general-purpose recursive parser and should be able to handle all OneFS stats.
- Clean up ugly global variables, add `-version` flag

### Bug fixes

- Fix "isInvalidStat()"
  - The collector tries to avoid collecting certain "change notify" stats because their definition is problematic and they can skew latency data. The code was checking fields instead of tags and did not remove them.

## v0.27 - Wed Sep 17 14:42:07 2025 -0700

### New features

- Add support for the client summary stats endpoint

## v0.26 - Thu Aug 28 13:29:37 2025 -0700

### Bug fixes

- Fix prometheus summary stats and tidy metricMap
  - The new protocol summary stats code was broken for Prometheus.
  - Clean up/simplify metricMap after the points refactor.
- Fix nasty priority queue bug in v0.25

### New features

- Force socket reuseaddr/reuseport on Prometheus endpoints
  - Previously, the collector would fail to bind the Prometheus endpoints if it was restarted within the TCP TIME_WAIT window.

### Security fixes

- Github automation: restrict access permissions for the workflow/automation to readonly.
- Bump golang.org/x/net from 0.34.0 to 0.38.0 to fix minor security issues.

## v0.25 - Mon Feb 17 15:56:22 2025 -0800

### New features

- Add support for protocol summary stats
  - The stats engine offers several summary statistics endpoints. This commit adds initial support for the protocol summary statistics endpoint.

> [!IMPORTANT]
> Although the summary stats default to disabled (meaning the older config files are compatible)
> this is flagged as a breaking change to force updating the config file.

### Security fixes

> [!IMPORTANT]
> The following changes mean that a minimum version of Golang 1.21 is required to build.

- Update dependencies to latest versions
  - Fixes dependabot alerts against golang.org/x/net andgoogle.golang.org/protobuf
- Update go.yml
  - Update to Go version 1.21 for build because pulling in the latest dependencies updated to 1.21 and that made `go mod tidy` add a toochain directive to go.mod which is not handled by earlier versions.

## v0.24 - Fri Jan 17 08:22:17 2025 -0800

### Changes

- Refactor back end to separate OneFS-specfic code
  - No externally visible change in functionality

### Bug fixes

- Fix silly issue command-line parsing
  - We must parse the command line before trying to use any of the flags.
- Fix build issues with InfluxDBv1
  - The v1.11.4 tag in the influxdata/influxdb repository was removed (bad) and broke the go module dependency handling. Switch to the standalone InfluxDBv1 client.

## v0.23 - Thu Nov 16 12:37:07 2023 -0800

### New features

- Add InfluxDBv2 support
  - This is in parallel to the InfluxDBv1 support which remains available in the collector.

## v0.22 - Tue Nov 14 19:17:13 2023 -0800

### Changes

- Add options to preserve case for cluster names
- Add options to make backend retries configurable
- Trivial cleanups

## v0.21 - Mon Nov 13 11:47:43 2023 -0800

> [!IMPORTANT]
> Due to the logfile changes, this is a breaking change which will
> require a manual update of the configuration file post-upgrade.

### Major changes

- Add secret support and log to stdout support
  - Add secret support for sensitive information such as cluster passwords
  - Add logfile configuration to the config file
    - Logging to stdout is enabled via the log_to_stdout parameter
    - Logging to file can be configured in the config file and overridden at the command line
- Bump version, mark as breaking change

## v0.20 - Tue Nov 7 14:33:17 2023 -0800

### Changes

- Add basic http landing page
  - Add basic collector description and "/metrics" link.

## v0.19 - Wed Nov 1 13:53:41 2023 -0700

### Changes

- Plumb in support for TLS-encrypted endpoints
  - The code was already there but not plumbed in to the config file.
- Improve run instructions

## v0.18 - Mon Oct 30 14:30:11 2023 -0700

> [!IMPORTANT]
> The config rewrite is a breaking change which will require
> a manual update of the configuration file post-upgrade.

### Major changes

- Major config rewrite
  - This config change means older config files must be updated to be compatible
  - Renamed back end names (removed "_plugin")
  - Removed hacky "processor_args" inherited from the Python collector
  - Added config stanzas for InfluxDB and Prometheus

## v0.17 - Mon Oct 30 12:42:21 2023 -0700

### Major changes

- Added config version checking
  - Upcoming changes will break the config file format.
  - Added a version check to avoid unexplained breakage.

## v0.16 - Sat Oct 28 15:42:34 2023 -0700

### Security

- Updated all dependencies to latest versions

## v0.15 - Fri Oct 27 15:49:22 2023 -0700

### Major changes

- Version bump due to greatly simplifying the Prometheus code
  - This version should be functionally identical to the last version, but given the scopy of the changes, the version has been bumped

### Bug fixes

- Dependency update to fix HTTP/2 vulnerabilities

## v0.14 - Fri Oct 27 06:55:50 2023 -0700

### Bug fixes

- Reworked Prometheus support
  - The initial Prometheus support had a major issue where stats that had been collected at least once, but which did not appear in the current collection cycle were still exposed via the `/metrics` endpoint
  - Completely rewrote the code. Advantages of the rewritten code include
    - the collector now correctly exports the original metric timestamp for each metric, and
    - the collector now expires metrics based on the metadata that defines how long they are value so stale metrics correctly disappear from the `/metrics` endpoint
  - Add stat namespace and basename; stats all now begin with "isilon_stat_".
- Added missing install/config instructions
- Fixed maxretries config parsing
  - Config struct elements need to be public for the toml parser to be able to work correctly.
- Updated InfluxDB install instructions
  - The code base currently uses InfluxDB v1. v2 support will be merged shortly which will require a config change (mandatory authentication).

## v0.13 - Mon Sep 18 14:13:16 2023 -0700

### New features

- Added support for hardcoding the HTTP SD listen addr
  - If listen_addr is defined in the HTTP SD config stanza, it will be used otherwise the code will try to find and use a routable external IP address.

### Bug fixes

- Fixed HTTP SD output
  - Nodes in the target array need to be quoted.
- Added check for valid (nonzero) stat config
  - Avoids a nil pointer crash in the main loop on invalid config.
  - This is usually caused by a config file syntax error e.g., placing a toml config section in the middle of another section.

### Minor tweak to service metadata

Set job type to "isilon_stats" instead of "isilon" so we can distinguish between the regular stats and the partitioned performance stats

## v0.12 - Wed Aug 30 11:19:23 2023 -0700

### New features

- Added support for Prometheus HTTP SD discovery

## v0.11 - Tue Aug 1 17:04:51 2023 -0700

### Bug fixes

- Fixed prom plugin argument handling and http routing
  - Listener port is per-cluster so update plugin argument handling to remove port from the argument handler.
- Fixed crash caused by incorrectly sharing the global http mux. Code now correctly registers a separate routing mux for each http listener rather than using the global mux.

## v0.10 - Fri Jul 28 08:14:30 2023 -0700

### Bug fixes

- Fixed cluster name breakage from Prometheus work

## v0.09 - Mon Jul 17 09:50:39 2023 -0700

### Major changes

- Moved prometheus listen port to cluster config
  - Each cluster needs to run a Prometheus metrics listener on a separate port. We could use a "base port" and increment but that would make it hard to know which port maps to which cluster over time.

### Bug fixes

- fixed missing initialization of bad stat set
- Squelched repeated error for missing stat info

## v0.08 - Tue Jun 20 11:43:43 2023 -0700

### New features

- Implemented Prometheus back end support

## v0.07 - Thu Sep 1 12:16:51 2022 -0700

### New features

- Added support for InfluxDB authentication (#3)
- Made retry limit configurable

### Bug fixes

- Fixed "Handle missing stats properly" (#6)
  - If stats are not found while we are parsing the stat update times, remove them from the groups because a missing stat will cause retrieval of all other stats in a single request to fail.
- Fixed "Ignore change notify" (#4)
  - Drop change notify stats so they don't pollute the latency statistics with misleading numbers
- Removed deprectated stat from config
  - The `node.memory.cache` statistic was removed from OneFS (and was always zero for several releases prior to its removal). Removed from the sample config file.

## v0.06 - Thu Feb 25 11:26:45 2021 -0800

### New features

- Added exponential backoff+unlimited retry when reading stats so that a cluster being temporarily unreachable does not halt stat collection

Occasionally, clusters will be unreachable for more than 30 minutes for various reasons e.g. power or network outage.
We now back off up to ~20 minutes per attempt but there is no limit on retry count.

## v0.05 - Thu Feb 21 16:04:22 2019 -0800

### New features

- Added configurable API authentication support ("basic-auth" or "session")
- Added support for disabling a cluster via `disabled = true` in the `[[cluster]]` stanza of the configuration file
- Added support for discard backend that throws away the stats

## v0.04 - Tue Jul 3 10:27:07 2018 -0700

### Major changes

- Restore support for session-based auth

## v0.03 - Sat Jan 27 17:58:50 2018 -0800

### New features

- support for configurable poll timing per group

## v0.02 - Tue Jan 16 19:33:21 2018 -0800

### Major changes

- session auth removed due to issues with low Apache session timeout (authentication reverted to basic auth only)

### New features

- add support for pluggable back ends
- collector handles re-authentication

## v0.01 - Sat Jan 13 12:30:20 2018 -0800

### Initial release of the Golang OneFS stats collector
