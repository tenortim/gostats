package main

import (
	"log"

	"github.com/BurntSushi/toml"
)

type tomlConfig struct {
	Global    globalConfig
	Cluster   []cluster
	StatGroup []statgroup
}

type globalConfig struct {
	Processor        string   `toml:"stats_processor"`
	ProcessorArgs    []string `toml:"stats_processor_args"`
	ActiveStatGroups []string `toml:"active_stat_groups"`
	MinUpdateInvtl   int      `toml:"min_update_interval_override"`
}

type cluster struct {
	Name     string
	Username string
	Password string
	Address  string
	SSLCheck bool `toml:"verify-ssl"`
}

type statgroup struct {
	Name        string
	UpdateIntvl string `toml:"update_interval"`
	Stats       []string
}

func main() {
	var conf tomlConfig
	_, err := toml.DecodeFile("example_isi_data_insights_d.toml", &conf)
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Printf("Config parsed\n%+v\n", conf)

}
