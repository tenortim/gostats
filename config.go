package main

// stats project config handling

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// If not overridden, we will only poll every minUpdateInterval seconds
const defaultMinUpdateInterval = 30

// Default retry limit
const defaultMaxRetries = 8
const processorDefaultMaxRetries = 8
const processorDefaultRetryIntvl = 5

// Default Normalizaion of ClusterNames
const defaultPreserveCase = false

// tomlConfig defines the top-level structure of the config file
type tomlConfig struct {
	Global       globalConfig
	Logging      loggingConfig     `toml:"logging"`
	InfluxDB     influxDBConfig    `toml:"influxdb"`
	InfluxDBv2   influxDBv2Config  `toml:"influxdbv2"`
	Prometheus   prometheusConfig  `toml:"prometheus"`
	PromSD       promSdConf        `toml:"prom_http_sd"`
	Clusters     []clusterConf     `toml:"cluster"`
	SummaryStats summaryStatConfig `toml:"summary_stats"`
	StatGroups   []statGroupConf   `toml:"statgroup"`
}

// globalConfig defines the global settings in the config file
type globalConfig struct {
	Version             string   `toml:"version"`
	Processor           string   `toml:"stats_processor"`
	ProcessorMaxRetries int      `toml:"stats_processor_max_retries"`
	ProcessorRetryIntvl int      `toml:"stats_processor_retry_interval"`
	MinUpdateInvtl      int      `toml:"min_update_interval_override"`
	MaxRetries          int      `toml:"max_retries"`
	ActiveStatGroups    []string `toml:"active_stat_groups"`
	PreserveCase        bool     `toml:"preserve_case"`    // enable/disable normalization of Cluster Names
	IncludeDegraded     bool     `toml:"include_degraded"` // include degraded status tag in metrics
}

// loggingConfig defines the logging settings in the config file
type loggingConfig struct {
	LogFile       *string `toml:"logfile"`
	LogFileFormat *string `toml:"log_file_format"`
	LogLevel      *string `toml:"log_level"`
	LogToStdout   bool    `toml:"log_to_stdout"`
}

// influxDBConfig defines the InfluxDB settings in the config file
type influxDBConfig struct {
	Host          string `toml:"host"`
	Port          string `toml:"port"`
	Database      string `toml:"database"`
	Authenticated bool   `toml:"authenticated"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
}

// influxDBv2Config defines the InfluxDBv2 settings in the config file
type influxDBv2Config struct {
	Host   string `toml:"host"`
	Port   string `toml:"port"`
	Org    string `toml:"org"`
	Bucket string `toml:"bucket"`
	Token  string `toml:"access_token"`
}

// prometheusConfig defines the Prometheus settings in the config file
type prometheusConfig struct {
	Authenticated bool   `toml:"authenticated"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
	TLSCert       string `toml:"tls_cert"`
	TLSKey        string `toml:"tls_key"`
}

// promSdConf defines the Prometheus HTTP Service Discovery settings in the config file
type promSdConf struct {
	Enabled    bool
	ListenAddr string `toml:"listen_addr"`
	SDport     uint64 `toml:"sd_port"`
}

// clusterConf defines the per-cluster settings in the config file
type clusterConf struct {
	Hostname       string  // cluster name/ip; ideally use a SmartConnect name
	Username       string  // account with the appropriate PAPI roles
	Password       string  // password for the account
	AuthType       string  // authentication type: "session" or "basic-auth"
	SSLCheck       bool    `toml:"verify-ssl"` // turn on/off SSL cert checking to handle self-signed certificates
	Disabled       bool    // if set, disable collection for this cluster
	PrometheusPort *uint64 `toml:"prometheus_port"` // If using the Prometheus collector, define the listener port for the metrics handler
	PreserveCase   *bool   `toml:"preserve_case"`   // Overwrite normalization of Cluster Name
}

// summaryStatConfig defines whether protocol and/or client summary stats are collected
type summaryStatConfig struct {
	Protocol bool // protocol summary stats enabled?
	Client   bool // client summary stats enabled?
}

// The collector partitions the stats to be collected into two tiers.
// At the top level, there are named groups and each group consists of a subset of stats.
// This facilitates grouping related stats and enabling/disabling collection
// by simply adding/removing the group name to the top-level set.
type statGroupConf struct {
	Name        string
	UpdateIntvl string `toml:"update_interval"`
	Stats       []string
}

// validateConfigVersion checks the version of the config file to ensure that it is
// compatible with this version of the collector
// If not, it is a fatal error
func validateConfigVersion(confVersion string) {
	if confVersion == "" {
		die("The collector requires a versioned config file (see the example config)")
	}
	v := strings.TrimLeft(confVersion, "vV")
	switch v {
	// last breaking change was the major logging rewrite in v0.31
	case "0.31", "0.32", "0.33":
		return
	}
	die("Config file version is not compatible with this collector version", slog.String("config file version", confVersion), slog.String("collector version", Version))
}

// mustReadConfig reads the config file or exits the program is this fails
func mustReadConfig(configFileName string) tomlConfig {
	var conf tomlConfig
	conf.Global.MaxRetries = defaultMaxRetries
	conf.Global.ProcessorMaxRetries = processorDefaultMaxRetries
	conf.Global.ProcessorRetryIntvl = processorDefaultRetryIntvl
	conf.Global.MinUpdateInvtl = defaultMinUpdateInterval
	conf.Global.PreserveCase = defaultPreserveCase

	_, err := toml.DecodeFile(configFileName, &conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: failed to read config file %s\nError: %v\nExiting\n", os.Args[0], configFileName, err.Error())
		os.Exit(1)
	}
	// Validate config version
	validateConfigVersion(conf.Global.Version)

	// If retries is 0 or negative, make it effectively infinite
	if conf.Global.MaxRetries <= 0 {
		conf.Global.MaxRetries = math.MaxInt
	}
	if conf.Global.ProcessorMaxRetries <= 0 {
		conf.Global.ProcessorMaxRetries = math.MaxInt
	}

	return conf
}

const envPrefix = "$env:"

// secretFromEnv checks if the string starts with $env: and if so, looks up
// the rest of the string as an environment variable and returns its value.
// If the env var is not set, an error is returned.
// If the string does not start with $env:, it is returned unchanged.
func secretFromEnv(s string) (string, error) {
	if !strings.HasPrefix(s, envPrefix) {
		return s, nil
	}
	envvar := strings.TrimPrefix(s, envPrefix)
	secret, ok := os.LookupEnv(envvar)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", envvar)
	}
	return secret, nil
}
