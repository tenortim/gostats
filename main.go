package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	logging "github.com/op/go-logging"
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

var log = logging.MustGetLogger("gostats")

type loglevel logging.Level

var logFileName = flag.String("logfile", "./gostats.log", "path name to log file")
var logLevel = loglevel(logging.NOTICE)

func (l *loglevel) String() string {
	var level logging.Level
	level = logging.Level(*l)
	return level.String()
}

func (l *loglevel) Set(value string) error {
	level, err := logging.LogLevel(value)
	if err != nil {
		return err
	}
	*l = loglevel(level)
	return nil
}

func init() {
	// tie log-level variable into flag parsing
	flag.Var(&logLevel, "loglevel", "default log level [CRITICAL|ERROR|WARNING|NOTICE|INFO|DEBUG]")
}

func main() {
	// parse command line
	flag.Parse()

	// set up logging
	setupLogging()

	// read in our config
	var conf tomlConfig
	_, err := toml.DecodeFile("idic.toml", &conf)
	if err != nil {
		log.Fatal(err)
	}
	log.Info("Successfully read config file")

	// Determine which stats to poll
	statgroups := make(map[string][]string)
	for _, sg := range conf.StatGroup {
		statgroups[sg.Name] = sg.Stats
	}
	// validate active groups
	asg := []string{}
	for _, group := range conf.Global.ActiveStatGroups {
		if _, ok := statgroups[group]; !ok {
			log.Warningf("Active stat group %q not found - removing\n", group)
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
			log.Infof("starting collect for cluster %s", cl.Name)
			defer wg.Done()
			statsloop(cl, conf.Global, stats)
		}(cl)
	}
	wg.Wait()
}

func setupLogging() {
	f, err := os.OpenFile(*logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gostats: unable to open log file %s for output - %s", *logFileName, err)
		os.Exit(2)
	}
	backend := logging.NewLogBackend(f, "", 0)
	var format = logging.MustStringFormatter(
		`%{time:2006-01-02T15:04:05Z07:00} %{shortfile} %{level} %{message}`,
	)
	backendFormatter := logging.NewBackendFormatter(backend, format)
	backendLeveled := logging.AddModuleLevel(backendFormatter)
	backendLeveled.SetLevel(logging.Level(logLevel), "")
	logging.SetBackend(backendLeveled)
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
		log.Errorf("Authentication to cluster %q failed: %v", cluster.Name, err)
		return
	}

	// Need to be able to parse multiple backends - hardcode for now
	if gc.Processor != "influxdb_plugin" {
		log.Errorf("Unrecognized backend plugin name: %q", gc.Processor)
		return
	}
	// XXX - need to pull actual name from API
	var ss = InfluxDBSink{
		Cluster: cluster.Name,
	}
	err = ss.Init(gc.ProcessorArgs)
	if err != nil {
		log.Errorf("Unable to initialize InfluxDB plugin: %v", err)
		return
	}

	// loop collecting and pushing stats
	for {
		nextTime := time.Now().Add(30 * time.Second)
		// Collect one set of stats
		sr, err := c.GetStats(stats)
		if err != nil {
			log.Errorf("Failed to retrieve stats for cluster %q: %v\n", cluster.Name, err)
			return
		}

		err = ss.WriteStats(sr)
		if err != nil {
			log.Errorf("Failed to write stats to database: %s", err)
			return
		}
		time.Sleep(nextTime.Sub(time.Now()))
	}
}
