package main

// stats project config handling

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

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
}

type clusterConf struct {
	Hostname string
	Username string
	Password string
	SSLCheck bool `toml:"verify-ssl"`
}

type statGroupConf struct {
	Name        string
	UpdateIntvl string `toml:"update_interval"`
	Stats       []string
}

// all stat config information
type statConf struct {
	statGroups       map[string]statGroupDetail
	activeStatGroups []string
	stats            []string
}

func mustReadConfig() tomlConfig {
	var conf tomlConfig
	_, err := toml.DecodeFile(*configFileName, &conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: unable to read config file %s, exiting\n", os.Args[0], *configFileName)
		// don't call log.Fatal so goimports doesn't get confused and try to add "log" to the imports
		log.Critical(err)
		os.Exit(1)
	}
	return conf
}
