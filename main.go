package main

import (
	"container/heap"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Version is the released program version
const Version = "0.35"
const userAgent = "gostats/" + Version

const (
	authtypeBasic   = "basic-auth"
	authtypeSession = "session"
)
const defaultAuthType = authtypeSession

// Config file plugin names
const (
	discardPluginName  = "discard"
	influxPluginName   = "influxdb"
	influxV2PluginName = "influxdbv2"
	promPluginName     = "prometheus"
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

// debugging flags
var checkStatReturn = flag.Bool("check-stat-return",
	false,
	"Verify that the api returns results for every stat requested")

func die(msg string, args ...any) {
	log.Log(context.Background(), LevelFatal, msg, args...)
	os.Exit(1)
}

func main() {
	setupEarlyLogging()
	logFileName := flag.String("logfile", "", "pathname of log file")
	configFileName := flag.String("config-file", "idic.toml", "pathname of config file")
	versionFlag := flag.Bool("version", false, "Print application version")
	logLevel := flag.String("loglevel", "", "log level [CRITICAL|ERROR|WARNING|NOTICE|INFO|DEBUG|TRACE]")
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
	setupLogging(conf.Logging, *logLevel, *logFileName)

	// create a context that is cancelled on SIGTERM or SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// announce ourselves
	log.Log(ctx, LevelNotice, "Starting gostats", slog.String("version", Version))

	// Ensure the config contains at least one stat to poll
	if len(conf.StatGroups) == 0 {
		log.Error("No stat groups found in config file. Unable to start collection")
		return
	}

	// Determine which stats to poll
	log.Info("Parsing stat groups and stats")
	sg := parseStatConfig(conf)

	// ugly, but we have to do this here since it's global, not a per-cluster
	if conf.Global.Processor == promPluginName && conf.PromSD.Enabled {
		if err := startPromSdListener(ctx, conf); err != nil {
			log.Error("Failed to start Prometheus SD listener", slog.String("error", err.Error()))
		}
	}

	// start collecting from each defined and enabled cluster
	var wg sync.WaitGroup
	for ci, cl := range conf.Clusters {
		if cl.Disabled {
			log.Info("skipping disabled cluster", slog.String("cluster", cl.Hostname))
			continue
		}
		wg.Add(1)
		go func(ci int, cl clusterConf) {
			log.Info("spawning collection loop", slog.String("cluster", cl.Hostname))
			defer wg.Done()
			statsloop(ctx, &conf, ci, sg)
			log.Info("collection loop ended", slog.String("cluster", cl.Hostname))
		}(ci, cl)
	}
	wg.Wait()
	log.Log(ctx, LevelNotice, "All collectors complete - exiting")
}

// parseStatConfig parses the stat-collection TOML config
// note we can't configure update interval here because we don't yet have any
// cluster connections and the values may vary by OS release so we want to
// pull the refresh info directly from each cluster (in statsloop)
func parseStatConfig(conf tomlConfig) map[string]statGroup {
	allStatGroups := make(map[string]statGroup)
	statGroups := make(map[string]statGroup)
	for _, sg := range conf.StatGroups {
		log.Debug("Parsing stat group detail", slog.String("group", sg.Name))
		sgr := parseUpdateIntvl(sg.UpdateIntvl, conf.Global.MinUpdateInvtl)
		sgd := statGroup{sgr, sg.Stats}
		allStatGroups[sg.Name] = sgd
	}

	// validate active groups
	log.Debug("Validating active stat group names")
	asg := []string{}
	for _, group := range conf.Global.ActiveStatGroups {
		if _, ok := allStatGroups[group]; !ok {
			log.Warn("Active stat group not found - removing\n", slog.String("group", group))
			continue
		}
		asg = append(asg, group)
	}

	// ensure that each stat only appears in one (active) group
	// we could check to see if the multipliers/times match, but it's simpler
	// to just treat this as an error since there's no reason for the duplication
	log.Debug("Checking for duplicate stat names")
	allstats := make(map[string]bool)
	for _, sg := range asg {
		for _, stat := range allStatGroups[sg].stats {
			if allstats[stat] {
				die("stat found in multiple stat groups. Please correct and retry.", slog.String("stat", stat))
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
			log.Warn("unable to parse interval multiplier, setting to 1", slog.String("interval", interval))
			return dr
		}
		return sgRefresh{multiplier, 0.0}
	}
	absTime, err := strconv.ParseFloat(interval, 64)
	if err != nil {
		log.Warn("unable to parse interval value, setting to 1x multiplier", slog.String("interval", interval))
		return dr
	}
	if absTime < float64(minIntvl) {
		log.Warn("absolute update time < minimum update time. Clamping to minimum",
			slog.Float64("absolute update time", absTime), slog.Int("minimum update time", minIntvl))
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
func statsloop(ctx context.Context, config *tomlConfig, ci int, sg map[string]statGroup) {
	var err error
	var password string
	var ss DBWriter // ss = stats sink

	cc := config.Clusters[ci]
	gc := config.Global

	var preserveCase bool

	if cc.PreserveCase == nil { // check for cluster overwrite setting of PreserveCase, default and to global setting
		preserveCase = gc.PreserveCase
	} else {
		preserveCase = *cc.PreserveCase
	}

	// Connect to the cluster
	authtype := cc.AuthType
	if authtype == "" {
		log.Info("No authentication type defined, using default", slog.String("default", authtypeSession), slog.String("cluster", cc.Hostname))
		authtype = defaultAuthType
	}
	if authtype != authtypeSession && authtype != authtypeBasic {
		log.Warn("Invalid authentication type, using default", slog.String("authtype", authtype), slog.String("default", authtypeSession), slog.String("cluster", cc.Hostname))
		authtype = defaultAuthType
	}
	if cc.Username == "" || cc.Password == "" {
		log.Error("Username and password must not be null", slog.String("cluster", cc.Hostname))
		return
	}
	password, err = secretFromEnv(cc.Password)
	if err != nil {
		log.Error("Unable to retrieve password from environment", slog.String("cluster", cc.Hostname), slog.String("error", err.Error()))
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
		PreserveCase: preserveCase,
	}
	if err = c.Connect(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Error("Connection failed", slog.String("cluster", c.Hostname), slog.String("error", err.Error()))
		}
		return
	}
	log.Info("Connected", slog.String("cluster", c.ClusterName), slog.String("version", c.OSVersion))

	log.Info("Fetching stat information", slog.String("cluster", c.ClusterName), slog.String("version", c.OSVersion))
	sd := c.fetchStatDetails(ctx, sg)

	// divide stats into buckets based on update interval
	log.Info("Calculating stat refresh times", slog.String("cluster", c.ClusterName))
	statBuckets := calcBuckets(c, gc.MinUpdateInvtl, sg, sd)
	if len(statBuckets) == 0 {
		log.Error("No stat buckets found. Check your config file", slog.String("cluster", c.ClusterName))
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
	}
	heap.Init(&pq)

	// Configure/initialize backend database writer
	ss, err = getDBWriter(gc.Processor)
	if err != nil {
		log.Error("failed to obtain backend", slog.String("backend", gc.Processor), slog.String("error", err.Error()))
		return
	}
	err = ss.Init(ctx, c.ClusterName, config, ci, sd)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Error("Unable to initialize backend", slog.String("backend", gc.Processor), slog.String("error", err.Error()))
		}
		return
	}

	// loop collecting and pushing stats
	log.Info("Starting stat collection loop", slog.String("cluster", c.ClusterName))
	for {
		nextItem := heap.Pop(&pq).(*Item)
		curTime := time.Now()
		nextTime := nextItem.priority
		if curTime.Before(nextTime) {
			select {
			case <-time.After(nextTime.Sub(curTime)):
			case <-ctx.Done():
				log.Log(ctx, LevelNotice, "shutting down stats collection", slog.String("cluster", c.ClusterName))
				return
			}
		}
		// Collect one set of stats
		log.Debug("start stat collection", slog.String("cluster", c.ClusterName))
		if nextItem.value.stattype == StatTypeRegularStat {
			var sr []StatResult
			stats := nextItem.value.sts.stats
			readFailCount := 0
			const maxRetryTime = time.Second * 1280
			retryTime := time.Second * 10
			for {
				sr, err = c.GetStats(ctx, stats)
				if err == nil {
					break
				}
				readFailCount++
				if !errors.Is(err, context.Canceled) {
					log.Error("Failed to retrieve stats", slog.String("cluster", c.ClusterName), slog.String("error", err.Error()),
						slog.Int("retry count", readFailCount), slog.Duration("retry time", retryTime))
					if readFailCount >= c.maxRetries {
						log.Warn("cluster may be down or unreachable", slog.String("cluster", c.ClusterName),
							slog.Int("retry count", readFailCount))
					}
				}
				select {
				case <-time.After(retryTime):
				case <-ctx.Done():
					log.Log(ctx, LevelNotice, "shutting down stats collection", slog.String("cluster", c.ClusterName))
					return
				}
				if retryTime < maxRetryTime {
					retryTime *= 2
				}
			}
			if *checkStatReturn {
				verifyStatReturn(c.ClusterName, stats, sr)
			}
			nextItem.priority = nextItem.priority.Add(nextItem.value.sts.interval)
			heap.Push(&pq, nextItem)
			log.Debug("start writing stats to back end", slog.String("cluster", c.ClusterName))
			// write stats, now with retries
			err = c.WriteStats(ctx, gc, ss, sr)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Error("unable to write stats to database, stopping collection", slog.String("cluster", c.ClusterName))
				}
				return
			}
		} else if nextItem.value.stattype == StatTypeSummaryStatProtocol {
			log.Debug("collecting protocol summary stats", slog.String("cluster", c.ClusterName))
			ssp, err := c.GetSummaryProtocolStats(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Error("failed to collect summary protocol stats", slog.String("cluster", c.ClusterName), slog.String("error", err.Error()))
				}
			} else {
				name := summaryStatsBasename + "protocol"
				points := make([]Point, len(ssp))
				for i, stat := range ssp {
					var fa []ptFields
					var ta []ptTags
					fields, tags := decodeProtocolSummaryStat(c.ClusterName, stat)
					fa = append(fa, fields)
					ta = append(ta, tags)
					points[i] = Point{name: name, time: stat.Time, fields: fa, tags: ta}
				}
				log.Debug("start writing protocol summary stats to back end", slog.String("cluster", c.ClusterName))
				err = ss.WritePoints(ctx, points)
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						log.Error("unable to write protocol summary stats to database, stopping collection", slog.String("cluster", c.ClusterName))
					}
					return
				}
			}
			nextItem.priority = nextItem.priority.Add(time.Second * 5) // Summary stats are all on a 5-second collection interval
			heap.Push(&pq, nextItem)
		} else if nextItem.value.stattype == StatTypeSummaryStatClient {
			log.Debug("collecting client summary stats", slog.String("cluster", c.ClusterName))
			ssc, err := c.GetSummaryClientStats(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Error("failed to collect summary client stats", slog.String("cluster", c.ClusterName), slog.String("error", err.Error()))
				}
			} else {
				name := summaryStatsBasename + "client"
				points := make([]Point, len(ssc))
				for i, stat := range ssc {
					var fa []ptFields
					var ta []ptTags
					fields, tags := decodeClientSummaryStat(c.ClusterName, stat)
					fa = append(fa, fields)
					ta = append(ta, tags)
					points[i] = Point{name: name, time: stat.Time, fields: fa, tags: ta}
				}
				log.Debug("start writing client summary stats to back end", slog.String("cluster", c.ClusterName))
				err = ss.WritePoints(ctx, points)
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						log.Error("unable to write client summary stats to database, stopping collection", slog.String("cluster", c.ClusterName))
					}
					return
				}
			}
			nextItem.priority = nextItem.priority.Add(time.Second * 5) // Summary stats are all on a 5-second collection interval
			heap.Push(&pq, nextItem)
		} else {
			die("logic error: unknown summary stat type", slog.Int("stat type", int(nextItem.value.stattype)))
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
		absTime := sg[group].absTime
		if absTime != 0 {
			// these were already clamped to no less than the minimum in the
			// global config parsing so nothing to do here
			d := time.Duration(absTime) * time.Second
			stm[d] = append(stm[d], sg[group].stats...)
			continue
		}
		multiplier := sg[group].multiplier
		if multiplier == 0 {
			die("logic error: both multiplier and absTime are zero")
		}
		for _, stat := range sg[group].stats {
			statDetail := sd[stat]
			if !statDetail.valid {
				log.Warn("skipping invalid stat", slog.String("cluster", c.ClusterName), slog.String("stats", stat))
				continue
			}
			sui := statDetail.updateIntvl
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
				die("logic error: zero duration", slog.String("stat", stat), slog.Float64("update interval", sui), slog.Float64("multiplier", multiplier))
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
	case discardPluginName:
		return GetDiscardWriter(), nil
	case influxPluginName:
		return GetInfluxDBWriter(), nil
	case influxV2PluginName:
		return GetInfluxDBv2Writer(), nil
	case promPluginName:
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
		log.Error("Stats collection missing stats", slog.String("cluster", cluster), slog.Any("missing", missing))
	}
}
