package main

import (
	"fmt"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/influxdata/influxdb/client/v2"
)

// InfluxDBSink defines the data to allow us talk to an InfluxDB database
type InfluxDBSink struct {
	cluster  string
	c        client.Client
	bpConfig client.BatchPointsConfig
	badStats mapset.Set[string]
}

// GetInfluxDBWriter returns an InfluxDB DBWriter
func GetInfluxDBWriter() DBWriter {
	return &InfluxDBSink{}
}

// Init initializes an InfluxDBSink so that points can be written
// The array of argument strings comprises host, port, database
func (s *InfluxDBSink) Init(cluster string, _ clusterConf, args []string, _ map[string]statDetail) error {
	var username, password string
	authenticated := false
	// args are host, port, database, and, optionally, username and password
	switch len(args) {
	case 3:
		authenticated = false
	case 5:
		authenticated = true
	default:
		return fmt.Errorf("InfluxDB Init() wrong number of args %d - expected 3 or 5", len(args))
	}

	s.cluster = cluster
	host, port, database := args[0], args[1], args[2]
	if authenticated {
		username = args[3]
		password = args[4]
	}
	url := "http://" + host + ":" + port

	s.bpConfig = client.BatchPointsConfig{
		Database:  database,
		Precision: "s",
	}

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     url,
		Username: username,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("failed to create InfluxDB client - %v", err.Error())
	}
	s.c = c
	s.badStats = mapset.NewSet[string]()
	return nil
}

// WriteStats takes an array of StatResults and writes them to InfluxDB
func (s *InfluxDBSink) WriteStats(stats []StatResult) error {
	bp, err := client.NewBatchPoints(s.bpConfig)
	if err != nil {
		return fmt.Errorf("unable to create InfluxDB batch points - %v", err.Error())
	}
	for _, stat := range stats {
		var pts []*client.Point
		var fa []ptFields
		var ta []ptTags

		if stat.ErrorCode != 0 {
			if !s.badStats.Contains(stat.Key) {
				log.Warningf("Unable to retrieve stat %v from cluster %v, error %v", stat.Key, s.cluster, stat.ErrorString)
			}
			// add it to the set of bad (unavailable) stats
			s.badStats.Add(stat.Key)
			continue
		}
		fa, ta, err = DecodeStat(s.cluster, stat)
		if err != nil {
			// TODO consider trying to recover/handle errors
			log.Panicf("Failed to decode stat %+v: %s\n", stat, err)
		}
		for i, f := range fa {
			var pt *client.Point
			pt, err = client.NewPoint(stat.Key, ta[i], f, time.Unix(stat.UnixTime, 0).UTC())
			if err != nil {
				log.Warningf("failed to create point %q:%v", stat.Key, stat.Value)
				continue
			}
			pts = append(pts, pt)
		}
		if len(pts) > 0 {
			bp.AddPoints(pts)
		}
	}
	// write the batch
	err = s.c.Write(bp)
	if err != nil {
		return fmt.Errorf("failed to write batch of points - %v", err.Error())
	}
	return nil
}
