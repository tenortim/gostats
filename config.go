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
	Clusters   []clusterConf   `toml:"cluster"`
	StatGroups []statGroupConf `toml:"statgroup"`
}

type globalConfig struct {
	Processor        string   `toml:"stats_processor"`
	ProcessorArgs    []string `toml:"stats_processor_args"`
	ActiveStatGroups []string `toml:"active_stat_groups"`
	MinUpdateInvtl   int      `toml:"min_update_interval_override"`
	maxRetries       int      `toml:"max_retries"`
}

type clusterConf struct {
	Hostname string
	Username string
	Password string
	AuthType string
	SSLCheck bool `toml:"verify-ssl"`
	Disabled bool
}

type statGroupConf struct {
	Name        string
	UpdateIntvl string `toml:"update_interval"`
	Stats       []string
}

func mustReadConfig() tomlConfig {
	var conf tomlConfig
	conf.Global.maxRetries = defaultMaxRetries
	conf.Global.MinUpdateInvtl = defaultMinUpdateInterval
	_, err := toml.DecodeFile(*configFileName, &conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: unable to read config file %s, exiting\n", os.Args[0], *configFileName)
		// don't call log.Fatal so goimports doesn't get confused and try to add "log" to the imports
		log.Critical(err)
		os.Exit(1)
	}
	// If retries is 0 or negative, make it effectively infinite
	if conf.Global.maxRetries <= 0 {
		conf.Global.maxRetries = math.MaxInt
	}

	return conf
}
