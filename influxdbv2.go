package main

import (
	"fmt"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// InfluxDBv2Sink defines the data to allow us talk to an InfluxDBv2 database
type InfluxDBv2Sink struct {
	cluster  string
	c        influxdb2.Client
	writeAPI api.WriteAPI
	badStats mapset.Set[string]
}

// GetInfluxDBv2Writer returns an InfluxDBv2 DBWriter
func GetInfluxDBv2Writer() DBWriter {
	return &InfluxDBv2Sink{}
}

// Init initializes an InfluxDBv2Sink so that points can be written
func (s *InfluxDBv2Sink) Init(cluster string, config *tomlConfig, _ int, _ map[string]statDetail) error {
	s.cluster = cluster
	var err error
	ic := config.InfluxDBv2
	url := "http://" + ic.Host + ":" + ic.Port

	token := ic.Token
	if token == "" {
		return fmt.Errorf("InfluxDBv2 access token is missing or empty")
	}
	token, err = secretFromEnv(token)
	if err != nil {
		return fmt.Errorf("unable to retrieve InfluxDBv2 token from environment: %v", err.Error())
	}
	client := influxdb2.NewClient(url, token)
	writeAPI := client.WriteAPI(ic.Org, ic.Bucket)
	s.c = client
	s.writeAPI = writeAPI

	// Get errors channel
	errorsCh := writeAPI.Errors()
	// Create goroutine for reading and logging errors
	go func() {
		for err := range errorsCh {
			log.Errorf("InfluxDB async write error for cluster %s: %s\n", cluster, err.Error())
		}
	}()
	s.badStats = mapset.NewSet[string]()
	return nil
}

// WriteStats takes an array of StatResults and writes them to InfluxDBv2
func (s *InfluxDBv2Sink) WriteStats(stats []StatResult) error {
	for _, stat := range stats {
		var fa []ptFields
		var ta []ptTags
		var err error

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
			//var pt *client.Point
			//pt, err = client.NewPoint(stat.Key, ta[i], f, time.Unix(stat.UnixTime, 0).UTC())
			//if err != nil {
			//	log.Warningf("failed to create point %q:%v", stat.Key, stat.Value)
			//	continue
			//}
			pt := influxdb2.NewPoint(stat.Key, ta[i], f, time.Unix(stat.UnixTime, 0).UTC())
			s.writeAPI.WritePoint(pt)
		}
	}
	// write the batch
	s.writeAPI.Flush()
	return nil
}
