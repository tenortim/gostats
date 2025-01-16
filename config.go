package main

// stats project config handling

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// If not overridden, we will only poll every minUpdateInterval seconds
const defaultMinUpdateInterval = 30

// Default retry limit
const defaultMaxRetries = 8
const ProcessordefaultMaxRetries = 8
const ProcessorDefaultRetryIntvl = 5

// Default Normalizaion of ClusterNames
const defaultPreserveCase = false

// config file structures
type tomlConfig struct {
	Global       globalConfig
	InfluxDB     influxDBConfig    `toml:"influxdb"`
	InfluxDBv2   influxDBv2Config  `toml:"influxdbv2"`
	Prometheus   prometheusConfig  `toml:"prometheus"`
	PromSD       promSdConf        `toml:"prom_http_sd"`
	Clusters     []clusterConf     `toml:"cluster"`
	SummaryStats summaryStatConfig `toml:"summary_stats"`
	StatGroups   []statGroupConf   `toml:"statgroup"`
}

type globalConfig struct {
	Version             string   `toml:"version"`
	LogFile             *string  `toml:"logfile"`
	LogToStdout         bool     `toml:"log_to_stdout"`
	Processor           string   `toml:"stats_processor"`
	ProcessorMaxRetries int      `toml:"stats_processor_max_retries"`
	ProcessorRetryIntvl int      `toml:"stats_processor_retry_interval"`
	MinUpdateInvtl      int      `toml:"min_update_interval_override"`
	MaxRetries          int      `toml:"max_retries"`
	ActiveStatGroups    []string `toml:"active_stat_groups"`
	PreserveCase        bool     `toml:"preserve_case"` // enable/disable normalization of Cluster Names
}

type influxDBConfig struct {
	Host          string `toml:"host"`
	Port          string `toml:"port"`
	Database      string `toml:"database"`
	Authenticated bool   `toml:"authenticated"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
}

type influxDBv2Config struct {
	Host   string `toml:"host"`
	Port   string `toml:"port"`
	Org    string `toml:"org"`
	Bucket string `toml:"bucket"`
	Token  string `toml:"access_token"`
}

type prometheusConfig struct {
	Authenticated bool   `toml:"authenticated"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
	TLSCert       string `toml:"tls_cert"`
	TLSKey        string `toml:"tls_key"`
}

type promSdConf struct {
	Enabled    bool
	ListenAddr string `toml:"listen_addr"`
	SDport     uint64 `toml:"sd_port"`
}

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

type summaryStatConfig struct {
	Protocol bool // protocol summary stats enabled?
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

// mustReadConfig reads the config file or exits the program is this fails
func mustReadConfig() tomlConfig {
	var conf tomlConfig
	conf.Global.MaxRetries = defaultMaxRetries
	conf.Global.ProcessorMaxRetries = ProcessordefaultMaxRetries
	conf.Global.ProcessorRetryIntvl = ProcessorDefaultRetryIntvl
	conf.Global.MinUpdateInvtl = defaultMinUpdateInterval
	conf.Global.PreserveCase = defaultPreserveCase

	_, err := toml.DecodeFile(*configFileName, &conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: failed to read config file %s\nRrror %v\nExiting\n", os.Args[0], *configFileName, err.Error())
		os.Exit(1)
	}
	// If retries is 0 or negative, make it effectively infinite
	if conf.Global.MaxRetries <= 0 {
		conf.Global.MaxRetries = math.MaxInt
	}
	if conf.Global.ProcessorMaxRetries <= 0 {
		conf.Global.ProcessorMaxRetries = math.MaxInt
	}

	return conf
}

const ENVPREFIX = "$env:"

func secretFromEnv(s string) (string, error) {
	if !strings.HasPrefix(s, ENVPREFIX) {
		return s, nil
	}
	envvar := strings.TrimPrefix(s, ENVPREFIX)
	secret := os.Getenv(envvar)
	if secret == "" {
		return "", fmt.Errorf("unable to find environment variable %s to interpolate", envvar)
	}
	return secret, nil
}
