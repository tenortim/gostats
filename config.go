package main

// stats project config handling

import (
	"fmt"
	"math"
	"os"

	"github.com/BurntSushi/toml"
)

// If not overridden, we will only poll every minUpdateInterval seconds
const defaultMinUpdateInterval = 30

// Default retry limit
const defaultMaxRetries = 8

// config file structures
type tomlConfig struct {
	Global     globalConfig
	InfluxDB   influxDBConfig   `toml:"influxdb"`
	Prometheus prometheusConfig `toml:"prometheus"`
	PromSD     promSdConf       `toml:"prom_http_sd"`
	Clusters   []clusterConf    `toml:"cluster"`
	StatGroups []statGroupConf  `toml:"statgroup"`
}

type globalConfig struct {
	Version          string   `toml:"version"`
	Processor        string   `toml:"stats_processor"`
	MinUpdateInvtl   int      `toml:"min_update_interval_override"`
	MaxRetries       int      `toml:"max_retries"`
	ActiveStatGroups []string `toml:"active_stat_groups"`
}

type influxDBConfig struct {
	Host          string `toml:"host"`
	Port          string `toml:"port"`
	Database      string `toml:"database"`
	Authenticated bool   `toml:"authenticated"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
}

type prometheusConfig struct {
	Authenticated bool   `toml:"authenticated"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
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
}

// The collector partitiones the stats to be collected into two tiers.
// There are named groups and each group consists of a subset of stats.
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
	conf.Global.MinUpdateInvtl = defaultMinUpdateInterval
	_, err := toml.DecodeFile(*configFileName, &conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: unable to read config file %s, exiting\n", os.Args[0], *configFileName)
		// don't call log.Fatal so goimports doesn't get confused and try to add "log" to the imports
		log.Critical(err)
		os.Exit(1)
	}
	// If retries is 0 or negative, make it effectively infinite
	if conf.Global.MaxRetries <= 0 {
		conf.Global.MaxRetries = math.MaxInt
	}

	return conf
}
