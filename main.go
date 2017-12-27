package main

import (
	"log"
	"timw/isilon/gostats/papistats"
	"timw/isilon/gostats/statssink"

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
	_, err := toml.DecodeFile("idic.toml", &conf)
	if err != nil {
		log.Fatal(err)
	}

	// Need to be able to parse multiple backends - hardcode for now
	if conf.Global.Processor != "influxdb_plugin" {
		log.Fatalf("Unrecognized backend plugin name: %q", conf.Global.Processor)
	}
	// This will be in the per-cluster code eventually
	// Also will need to pull actual name from API
	var ss = statssink.InfluxDBSink{
		Cluster: conf.Cluster[0].Name,
	}
	err = ss.Init(conf.Global.ProcessorArgs)
	if err != nil {
		log.Fatalf("Unable to initialize InfluxDB plugin: %v", err)
	}

	// Determine which stats to poll
	statgroups := make(map[string][]string)
	for _, sg := range conf.StatGroup {
		statgroups[sg.Name] = sg.Stats
	}
	// validate active groups
	asg := []string{}
	for _, group := range conf.Global.ActiveStatGroups {
		if _, ok := statgroups[group]; !ok {
			log.Printf("Active stat group %q not found - removing\n", group)
			continue
		}
		asg = append(asg, group)
	}
	allstats := make(map[string]bool)
	for _, sg := range asg {
		for _, stat := range statgroups[sg] {
			allstats[stat] = true
		}
	}
	stats := []string{}
	for stat := range allstats {
		stats = append(stats, stat)
	}

	// Connect to the cluster
	cluster := conf.Cluster[0]
	c := &papistats.Cluster{
		AuthInfo: papistats.AuthInfo{
			Username: cluster.Username,
			Password: cluster.Password,
		},
		Hostname:  cluster.Address,
		Port:      8080,
		VerifySSL: cluster.SSLCheck,
	}
	if err = c.Authenticate(); err != nil {
		log.Panicf("Authentication to cluster %q failed: %v", cluster.Name, err)
	}

	// Collect one set of stats
	sr, err := c.GetStats(stats)
	if err != nil {
		log.Panicf("Failed to retrieve stats for cluster %q: %v\n", cluster.Name, err)
	}

	err = ss.WriteStats(sr)
	if err != nil {
		log.Panicf("Failed to write stats to database: %s", err)
	}
}
