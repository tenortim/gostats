package main

import (
	"log"
	"sync"
	"time"

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

	// start collecting from each defined cluster
	var wg sync.WaitGroup
	wg.Add(len(conf.Cluster))
	for _, cl := range conf.Cluster {
		go func(cl cluster) {
			defer wg.Done()
			statsloop(cl, conf.Global, stats)
		}(cl)
	}
	wg.Wait()
}

func statsloop(cluster cluster, gc globalConfig, stats []string) {
	var err error
	// connect/authenticate
	c := &Cluster{
		AuthInfo: AuthInfo{
			Username: cluster.Username,
			Password: cluster.Password,
		},
		Hostname:  cluster.Address,
		Port:      8080,
		VerifySSL: cluster.SSLCheck,
	}
	if err = c.Authenticate(); err != nil {
		log.Printf("Authentication to cluster %q failed: %v", cluster.Name, err)
		return
	}

	// Need to be able to parse multiple backends - hardcode for now
	if gc.Processor != "influxdb_plugin" {
		log.Printf("Unrecognized backend plugin name: %q", gc.Processor)
		return
	}
	// XXX - need to pull actual name from API
	var ss = InfluxDBSink{
		Cluster: cluster.Name,
	}
	err = ss.Init(gc.ProcessorArgs)
	if err != nil {
		log.Printf("Unable to initialize InfluxDB plugin: %v", err)
		return
	}

	// loop collecting and pushing stats
	for {
		nextTime := time.Now().Add(30 * time.Second)
		// Collect one set of stats
		sr, err := c.GetStats(stats)
		if err != nil {
			log.Printf("Failed to retrieve stats for cluster %q: %v\n", cluster.Name, err)
			return
		}

		err = ss.WriteStats(sr)
		if err != nil {
			log.Printf("Failed to write stats to database: %s", err)
			return
		}
		time.Sleep(nextTime.Sub(time.Now()))
	}
}
