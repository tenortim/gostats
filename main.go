package main

import (
	"container/heap"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	logging "github.com/op/go-logging"
)

// Version is the released program version
const Version = "0.15"
const userAgent = "gostats/" + Version

const (
	authtypeBasic   = "basic-auth"
	authtypeSession = "session"
)
const defaultAuthType = authtypeSession

// Config file plugin names
const (
	DISCARD_PLUGIN_NAME = "discard_plugin"
	INFLUX_PLUGIN_NAME  = "influxdb_plugin"
	PROM_PLUGIN_NAME    = "prometheus_plugin"
)

// parsed/populated stat structures
type sgRefresh struct {
	multiplier float64
	absTime    float64
}

type statGroup struct {
	sgRefresh
	stats []string
}

var log = logging.MustGetLogger("gostats")

type loglevel logging.Level

var logFileName = flag.String("logfile", "./gostats.log", "pathname of log file")
var logLevel = loglevel(logging.NOTICE)
var configFileName = flag.String("config-file", "idic.toml", "pathname of config file")

// debugging flags
var checkStatReturn = flag.Bool("check-stat-return",
	false,
	"Verify that the api returns results for every stat requested")

func (l *loglevel) String() string {
	level := logging.Level(*l)
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
	flag.Var(&logLevel,
		"loglevel",
		"default log level [CRITICAL|ERROR|WARNING|NOTICE|INFO|DEBUG]")
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

func main() {
	// parse command line
	flag.Parse()

	// set up logging
	setupLogging()

	// announce ourselves
	log.Noticef("Starting gostats version %s", Version)

	// read in our config
	conf := mustReadConfig()
	log.Info("Successfully read config file")

	// Ensure the config contains at least one stat to poll
	if len(conf.StatGroups) == 0 {
		log.Errorf("No stat groups found in config file. Unable to start collection")
		return
	}

	// Determine which stats to poll
	log.Info("Parsing stat groups and stats")
	sg := parseStatConfig(conf)
	// log.Infof("Parsed stats; %d stats will be collected", len(sc.stats))

	if conf.Global.Processor == PROM_PLUGIN_NAME && conf.PromSD.Enabled {
		startPromSdListener(conf)
	}

	// start collecting from each defined and enabled cluster
	var wg sync.WaitGroup
	for _, cl := range conf.Clusters {
		if cl.Disabled {
			log.Infof("skipping disabled cluster %q", cl.Hostname)
			continue
		}
		wg.Add(1)
		go func(cl clusterConf) {
			log.Infof("spawning collection loop for cluster %s", cl.Hostname)
			defer wg.Done()
			statsloop(cl, conf.Global, sg)
			log.Infof("collection loop for cluster %s ended", cl.Hostname)
		}(cl)
	}
	wg.Wait()
	log.Notice("All collectors complete - exiting")
}

// parseStatConfig parses the stat-collection TOML config
// note we can't configure update interval here because we don't yet have any
// cluster connections and the values may vary by OS release so we want to
// pull the refresh info directly from each cluster (in statsloop)
func parseStatConfig(conf tomlConfig) map[string]statGroup {
	allStatGroups := make(map[string]statGroup)
	statGroups := make(map[string]statGroup)
	for _, sg := range conf.StatGroups {
		log.Debugf("Parsing stat group detail for group %q", sg.Name)
		sgr := parseUpdateIntvl(sg.UpdateIntvl, conf.Global.MinUpdateInvtl)
		sgd := statGroup{sgr, sg.Stats}
		allStatGroups[sg.Name] = sgd
	}

	// validate active groups
	log.Debugf("Validating active stat group names")
	asg := []string{}
	for _, group := range conf.Global.ActiveStatGroups {
		if _, ok := allStatGroups[group]; !ok {
			log.Warningf("Active stat group %q not found - removing\n", group)
			continue
		}
		asg = append(asg, group)
	}

	// ensure that each stat only appears in one (active) group
	// we could check to see if the multipliers/times match, but it's simpler
	// to just treat this as an error since there's no reason for the duplication
	log.Debugf("Checking for duplicate stat names")
	allstats := make(map[string]bool)
	for _, sg := range asg {
		for _, stat := range allStatGroups[sg].stats {
			if allstats[stat] {
				log.Fatalf("stat %q found in multiple stat groups. Please correct and retry.", stat)
			}
			allstats[stat] = true
		}
	}

	for _, sg := range asg {
		statGroups[sg] = allStatGroups[sg]
	}
	// return statGroups here and parse out in statsloop
	return statGroups
}

func parseUpdateIntvl(interval string, minIntvl int) sgRefresh {
	// default is 1x multiplier (no effect)
	dr := sgRefresh{1.0, 0.0}
	if strings.HasPrefix(interval, "*") {
		if interval == "*" {
			return dr
		}
		multiplier, err := strconv.ParseFloat(interval[1:], 64)
		if err != nil {
			log.Warningf("unable to parse interval multiplier %q, setting to 1", interval)
			return dr
		}
		return sgRefresh{multiplier, 0.0}
	}
	absTime, err := strconv.ParseFloat(interval, 64)
	if err != nil {
		log.Warningf("unable to parse interval value %q, setting to 1x multiplier", interval)
		return dr
	}
	if absTime < float64(minIntvl) {
		log.Warningf("absolute update time %v < minimum update time %v. Clamping to minimum", absTime, minIntvl)
		absTime = float64(minIntvl)
	}
	return sgRefresh{0.0, absTime}
}

// a mapping of the update interval to the stats to collect at that rate
type statTimeSet struct {
	interval time.Duration
	stats    []string
}

func statsloop(cluster clusterConf, gc globalConfig, sg map[string]statGroup) {
	var err error
	var ss DBWriter

	// Connect to the cluster
	authtype := cluster.AuthType
	if authtype == "" {
		log.Infof("No authentication type defined for cluster %s, defaulting to %s", cluster.Hostname, authtypeSession)
		authtype = defaultAuthType
	}
	if authtype != authtypeSession && authtype != authtypeBasic {
		log.Warningf("Invalid authentication type %q for cluster %s, using default of %s", authtype, cluster.Hostname, authtypeSession)
		authtype = defaultAuthType
	}
	c := &Cluster{
		AuthInfo: AuthInfo{
			Username: cluster.Username,
			Password: cluster.Password,
		},
		AuthType:   authtype,
		Hostname:   cluster.Hostname,
		Port:       8080,
		VerifySSL:  cluster.SSLCheck,
		maxRetries: gc.MaxRetries,
	}
	if err = c.Connect(); err != nil {
		log.Errorf("Connection to cluster %s failed: %v", c.Hostname, err)
		return
	}
	log.Infof("Connected to cluster %s, version %s", c.ClusterName, c.OSVersion)

	log.Infof("Fetching stat information for cluster %s, version %s", c.ClusterName, c.OSVersion)
	sd := c.fetchStatDetails(sg)

	// divide stats into buckets based on update interval
	log.Infof("Calculating stat refresh times for cluster %s", c.ClusterName)
	statBuckets := calcBuckets(c, gc.MinUpdateInvtl, sg, sd)
	if len(statBuckets) == 0 {
		log.Errorf("No stat buckets found for cluster %s. Check your config file", c.ClusterName)
		return
	}

	// initial priority PriorityQueue
	startTime := time.Now()
	pq := make(PriorityQueue, len(statBuckets))
	i := 0
	for _, v := range statBuckets {
		pq[i] = &Item{
			value:    v, // statTimeSet
			priority: startTime,
			index:    i,
		}
		i++
	}
	heap.Init(&pq)

	// Configure/initialize backend database writer
	ss, err = getDBWriter(gc.Processor)
	if err != nil {
		log.Error(err)
		return
	}
	err = ss.Init(c.ClusterName, cluster, gc.ProcessorArgs, sd)
	if err != nil {
		log.Errorf("Unable to initialize %s plugin: %v", gc.Processor, err)
		return
	}

	// loop collecting and pushing stats
	log.Infof("Starting stat collection loop for cluster %s", c.ClusterName)
	for {
		nextItem := heap.Pop(&pq).(*Item)
		curTime := time.Now()
		nextTime := nextItem.priority
		if curTime.Before(nextTime) {
			time.Sleep(nextTime.Sub(curTime))
		}
		// Collect one set of stats
		log.Debugf("Cluster %s start collecting stats", c.ClusterName)
		var sr []StatResult
		stats := nextItem.value.stats
		readFailCount := 0
		const maxRetryTime = time.Second * 1280
		retryTime := time.Second * 10
		for {
			sr, err = c.GetStats(stats)
			if err == nil {
				break
			}
			readFailCount++
			log.Errorf("Failed to retrieve stats for cluster %q: %v - retry #%d in %v", c.ClusterName, err, readFailCount, retryTime)
			time.Sleep(retryTime)
			if retryTime < maxRetryTime {
				retryTime *= 2
			}
		}
		if *checkStatReturn {
			verifyStatReturn(c.ClusterName, stats, sr)
		}
		nextItem.priority = nextItem.priority.Add(nextItem.value.interval)
		heap.Push(&pq, nextItem)

		log.Debugf("Cluster %s start writing stats to back end", c.ClusterName)
		err = ss.WriteStats(sr)
		if err != nil {
			// TODO maybe implement backoff/error-handling here?
			log.Errorf("Failed to write stats to database: %s", err)
			return
		}
	}
}

// map out sets of stats to collect by update interval
func calcBuckets(c *Cluster, mui int, sg map[string]statGroup, sd map[string]statDetail) []statTimeSet {
	stm := make(map[time.Duration][]string)
	for group := range sg {
		absTime := sg[group].sgRefresh.absTime
		if absTime != 0 {
			// these were already clamped to no less than the minimum in the
			// global config parsing so nothing to do here
			d := time.Duration(absTime) * time.Second
			stm[d] = append(stm[d], sg[group].stats...)
			continue
		}
		multiplier := sg[group].sgRefresh.multiplier
		if multiplier == 0 {
			log.Panicf("logic error: both multiplier and absTime are zero")
		}
		for _, stat := range sg[group].stats {
			sd := sd[stat]
			if !sd.valid {
				log.Warningf("skipping invalid stat: '%v'", stat)
				continue
			}
			sui := sd.updateIntvl
			var d time.Duration
			if sui == 0 {
				// no defined update interval for this stat so use our default
				d = time.Duration(mui) * time.Second
			} else {
				intvlSecs := multiplier * sui
				if intvlSecs < float64(mui) {
					// clamp interval to at least the minimum
					intvlSecs = float64(mui)
				}
				d = time.Duration(intvlSecs) * time.Second
			}
			if d == 0 {
				log.Fatalf("logic error: zero duration: stat %q, update interval %v, multiplier %v", stat, sui, multiplier)
			}
			stm[d] = append(stm[d], stat)
		}
	}
	sts := make([]statTimeSet, len(stm))
	i := 0
	for k, v := range stm {
		sts[i] = statTimeSet{k, v}
		i++
	}
	return sts
}

// return a DBWriter for the given backend name
func getDBWriter(sp string) (DBWriter, error) {
	switch sp {
	case DISCARD_PLUGIN_NAME:
		return GetDiscardWriter(), nil
	case INFLUX_PLUGIN_NAME:
		return GetInfluxDBWriter(), nil
	case PROM_PLUGIN_NAME:
		return GetPrometheusWriter(), nil
	default:
		return nil, fmt.Errorf("unsupported backend plugin %q", sp)
	}
}

func verifyStatReturn(cluster string, stats []string, sr []StatResult) {
	resultNames := make(map[string]bool)
	missing := []string{}
	for _, result := range sr {
		resultNames[result.Key] = true
	}
	for _, stat := range stats {
		if !resultNames[stat] {
			missing = append(missing, stat)
		}
	}
	if len(missing) != 0 {
		log.Errorf("Stats collection for cluster %s failed to collect the following stats: %v", cluster, missing)
	}
}
