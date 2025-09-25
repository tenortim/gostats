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
const Version = "0.28"
const userAgent = "gostats/" + Version

const (
	authtypeBasic   = "basic-auth"
	authtypeSession = "session"
)
const defaultAuthType = authtypeSession

// Config file plugin names
const (
	DISCARD_PLUGIN_NAME  = "discard"
	INFLUX_PLUGIN_NAME   = "influxdb"
	INFLUXv2_PLUGIN_NAME = "influxdbv2"
	PROM_PLUGIN_NAME     = "prometheus"
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

var logLevel = loglevel(logging.NOTICE)

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

func backendFromFile(f *os.File) logging.Backend {
	backend := logging.NewLogBackend(f, "", 0)
	var format = logging.MustStringFormatter(
		`%{time:2006-01-02T15:04:05Z07:00} %{shortfile} %{level} %{message}`,
	)
	backendFormatter := logging.NewBackendFormatter(backend, format)
	backendLeveled := logging.AddModuleLevel(backendFormatter)
	backendLeveled.SetLevel(logging.Level(logLevel), "")
	return backendLeveled
}

func setupLogging(gc globalConfig, logFileName string) {
	// Up to two backends (one file, one stdout)
	backends := make([]logging.Backend, 0, 2)
	// default is to not log to file
	logfile := ""
	// is it set in the config file?
	if gc.LogFile != nil {
		logfile = *gc.LogFile
	}
	// Finally, if it was set on the command line, override the setting
	if logFileName != "" {
		logfile = logFileName
	}
	if logfile != "" {
		f, err := os.OpenFile(logfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gostats: unable to open log file %s for output - %s", logfile, err)
			os.Exit(2)
		}
		backends = append(backends, backendFromFile(f))
	}
	if gc.LogToStdout {
		backends = append(backends, backendFromFile(os.Stdout))
	}
	if len(backends) == 0 {
		fmt.Fprintf(os.Stderr, "gostats: no logging defined, unable to continue\nPlease configure logging in the config file and/or via the command line\n")
		os.Exit(3)
	}
	logging.SetBackend(backends...)
}

// validateConfigVersion checks the version of the config file to ensure that it is
// compatible with this version of the collector
// If not, it is a fatal error
func validateConfigVersion(confVersion string) {
	if confVersion == "" {
		log.Fatalf("The collector requires a versioned config file (see the example config)")
	}
	v := strings.TrimLeft(confVersion, "vV")
	switch v {
	// last breaking change was addition of summary stats in v0.25
	case "0.25", "0.26", "0.27", "0.28":
		return
	}
	log.Fatalf("Config file version %q is not compatible with this collector version %s", confVersion, Version)
}

func main() {
	logFileName := flag.String("logfile", "", "pathname of log file")
	configFileName := flag.String("config-file", "idic.toml", "pathname of config file")
	versionFlag := flag.Bool("version", false, "Print application version")
	// parse command line
	flag.Parse()

	// if version requested, print and exit
	if *versionFlag {
		fmt.Printf("gostats version: %s\n", Version)
		return
	}

	// read in our config
	conf := mustReadConfig(*configFileName)

	// set up logging
	setupLogging(conf.Global, *logFileName)

	// announce ourselves
	log.Noticef("Starting gostats version %s", Version)

	validateConfigVersion(conf.Global.Version)

	// Ensure the config contains at least one stat to poll
	if len(conf.StatGroups) == 0 {
		log.Errorf("No stat groups found in config file. Unable to start collection")
		return
	}

	// Determine which stats to poll
	log.Info("Parsing stat groups and stats")
	sg := parseStatConfig(conf)
	// log.Infof("Parsed stats; %d stats will be collected", len(sc.stats))

	// ugly, but we have to do this here since it's global, not a per-cluster
	if conf.Global.Processor == PROM_PLUGIN_NAME && conf.PromSD.Enabled {
		startPromSdListener(conf)
	}

	// start collecting from each defined and enabled cluster
	var wg sync.WaitGroup
	for ci, cl := range conf.Clusters {
		if cl.Disabled {
			log.Infof("skipping disabled cluster %q", cl.Hostname)
			continue
		}
		wg.Add(1)
		go func(ci int, cl clusterConf) {
			log.Infof("spawning collection loop for cluster %s", cl.Hostname)
			defer wg.Done()
			statsloop(&conf, ci, sg)
			log.Infof("collection loop for cluster %s ended", cl.Hostname)
		}(ci, cl)
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

// parseUpdateIntvl parses the update interval string from the config file
// and returns a struct with either a multiplier or an absolute time
// if the string is invalid, a default of 1x multiplier is returned
// if the absolute time is less than the minimum interval, it is clamped to
// the minimum
// valid formats are:
//
//	*<multiplier>  - multiplier of the stat's native update interval
//	<absolute time> - absolute time in seconds (float allowed)
//	*              - same as *1.0
//
// examples:
//
//	*2.5  - 2.5 times the stat's native update interval
//	30    - 30 seconds absolute time
//	*     - 1x multiplier (no effect)
//	5.5   - 5.5 seconds absolute time
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

// statsloop is the main collection loop for a single cluster
// it connects to the cluster, determines the stats to collect and their
// collection intervals, and then enters a loop collecting and writing
// stats to the backend database
func statsloop(config *tomlConfig, ci int, sg map[string]statGroup) {
	var err error
	var password string
	var ss DBWriter // ss = stats sink

	cc := config.Clusters[ci]
	gc := config.Global

	var normalize bool

	if cc.PreserveCase == nil { // check for cluster overwrite setting of PreserveCase, default and to global setting
		normalize = gc.PreserveCase
	} else {
		normalize = *cc.PreserveCase
	}

	// Connect to the cluster
	authtype := cc.AuthType
	if authtype == "" {
		log.Infof("No authentication type defined for cluster %s, defaulting to %s", cc.Hostname, authtypeSession)
		authtype = defaultAuthType
	}
	if authtype != authtypeSession && authtype != authtypeBasic {
		log.Warningf("Invalid authentication type %q for cluster %s, using default of %s", authtype, cc.Hostname, authtypeSession)
		authtype = defaultAuthType
	}
	if cc.Username == "" || cc.Password == "" {
		log.Errorf("Username and password for cluster %s must no be null", cc.Hostname)
		return
	}
	password, err = secretFromEnv(cc.Password)
	if err != nil {
		log.Errorf("Unable to retrieve password from environment for cluster %s: %v", cc.Hostname, err.Error())
		return
	}
	c := &Cluster{
		AuthInfo: AuthInfo{
			Username: cc.Username,
			Password: password,
		},
		AuthType:     authtype,
		Hostname:     cc.Hostname,
		Port:         8080,
		VerifySSL:    cc.SSLCheck,
		maxRetries:   gc.MaxRetries,
		PreserveCase: normalize,
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

	// initialize minHeap/pq with our time-based buckets
	startTime := time.Now()
	pq := make(PriorityQueue, len(statBuckets))
	for i := range statBuckets {
		value := PqValue{StatTypeRegularStat, &statBuckets[i]}
		pq[i] = &Item{
			value:    value, // statTimeSet
			priority: startTime,
			index:    i,
		}
		i++
	}
	i := len(pq)
	// add entries for summary stats
	if config.SummaryStats.Protocol {
		item := Item{
			value:    PqValue{StatTypeSummaryStatProtocol, nil},
			priority: startTime,
			index:    i,
		}
		pq = append(pq, &item)
		i++
	}
	if config.SummaryStats.Client {
		item := Item{
			value:    PqValue{StatTypeSummaryStatClient, nil},
			priority: startTime,
			index:    i,
		}
		pq = append(pq, &item)
		i++
	}
	heap.Init(&pq)

	// Configure/initialize backend database writer
	ss, err = getDBWriter(gc.Processor)
	if err != nil {
		log.Error(err)
		return
	}
	err = ss.Init(c.ClusterName, config, ci, sd)
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
		if nextItem.value.stattype == StatTypeRegularStat {
			var sr []StatResult
			stats := nextItem.value.sts.stats
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
			nextItem.priority = nextItem.priority.Add(nextItem.value.sts.interval)
			heap.Push(&pq, nextItem)
			log.Debugf("Cluster %s start writing stats to back end", c.ClusterName)
			// write stats, now with retries
			err = c.WriteStats(gc, ss, sr)
			if err != nil {
				log.Errorf("unable to write stats to database, stopping collection for cluster %s", c.ClusterName)
				return
			}
		} else if nextItem.value.stattype == StatTypeSummaryStatProtocol {
			log.Debugf("collecting protocol summary stats for cluster %s here", c.ClusterName)
			ssp, err := c.GetSummaryProtocolStats()
			if err != nil {
				log.Errorf("failed to collect summary protocol stats: %v", err)
			} else {
				name := summaryStatsBasename + "protocol"
				points := make([]Point, len(ssp))
				for i, ss := range ssp {
					var fa []ptFields
					var ta []ptTags
					fields, tags := DecodeProtocolSummaryStat(c.ClusterName, ss)
					fa = append(fa, fields)
					ta = append(ta, tags)
					points[i] = Point{name: name, time: ss.Time, fields: fa, tags: ta}
				}
				log.Debugf("Cluster %s start writing protocol summary stats to back end", c.ClusterName)
				err = ss.WritePoints(points)
				if err != nil {
					log.Errorf("unable to write protocol summary stats to database, stopping collection for cluster %s", c.ClusterName)
					return
				}
			}
			nextItem.priority = nextItem.priority.Add(time.Second * 5) // Summary stats are all on a 5-second collection interval
			heap.Push(&pq, nextItem)
		} else if nextItem.value.stattype == StatTypeSummaryStatClient {
			log.Debugf("collecting client summary stats for cluster %s here", c.ClusterName)
			ssc, err := c.GetSummaryClientStats()
			if err != nil {
				log.Errorf("failed to collect summary client stats: %v", err)
			} else {
				name := summaryStatsBasename + "client"
				points := make([]Point, len(ssc))
				for i, ss := range ssc {
					var fa []ptFields
					var ta []ptTags
					fields, tags := DecodeClientSummaryStat(c.ClusterName, ss)
					fa = append(fa, fields)
					ta = append(ta, tags)
					points[i] = Point{name: name, time: ss.Time, fields: fa, tags: ta}
				}
				log.Debugf("Cluster %s start writing client summary stats to back end", c.ClusterName)
				err = ss.WritePoints(points)
				if err != nil {
					log.Errorf("unable to write client summary stats to database, stopping collection for cluster %s", c.ClusterName)
					return
				}
			}
			nextItem.priority = nextItem.priority.Add(time.Second * 5) // Summary stats are all on a 5-second collection interval
			heap.Push(&pq, nextItem)
		} else {
			log.Panicf("logic error: unknown summary stat type %v", nextItem.value.stattype)
		}

	}
}

// calcBuckets calculates the collection buckets for the given cluster
// based on the stat groups, their multipliers/absolute times, and the
// individual stat update intervals
// returns a slice of statTimeSet structs, each containing a collection
// interval and the list of stats to collect at that interval
// if a stat is invalid for the cluster, it is skipped with a warning
// if no valid stats are found, an empty slice is returned
// mui is the minimum update interval in seconds from the global config
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
				log.Warningf("cluster %s: skipping invalid stat: '%v'", c.ClusterName, stat)
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

// getDBWriter returns a DBWriter implementation based on the plugin name
// returns an error if the plugin name is not recognized
func getDBWriter(sp string) (DBWriter, error) {
	switch sp {
	case DISCARD_PLUGIN_NAME:
		return GetDiscardWriter(), nil
	case INFLUX_PLUGIN_NAME:
		return GetInfluxDBWriter(), nil
	case INFLUXv2_PLUGIN_NAME:
		return GetInfluxDBv2Writer(), nil
	case PROM_PLUGIN_NAME:
		return GetPrometheusWriter(), nil
	default:
		return nil, fmt.Errorf("unsupported backend plugin %q", sp)
	}
}

// verifyStatReturn checks that all requested stats were returned by the API
// and logs an error if any are missing
// this is only called if the -check-stat-return flag is set
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
