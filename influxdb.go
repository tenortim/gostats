package main

import (
	"fmt"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	client "github.com/influxdata/influxdb1-client/v2"
)

// InfluxDBSink defines the data to allow us talk to an InfluxDB database
type InfluxDBSink struct {
	cluster  string
	client   client.Client
	bpConfig client.BatchPointsConfig
	badStats mapset.Set[string]
}

// GetInfluxDBWriter returns an InfluxDB DBWriter
func GetInfluxDBWriter() DBWriter {
	return &InfluxDBSink{}
}

// Init initializes an InfluxDBSink so that points can be written
func (s *InfluxDBSink) Init(cluster string, config *tomlConfig, _ int, _ map[string]statDetail) error {
	s.cluster = cluster
	var username, password string
	var err error
	ic := config.InfluxDB
	url := "http://" + ic.Host + ":" + ic.Port

	s.bpConfig = client.BatchPointsConfig{
		Database:  ic.Database,
		Precision: "s",
	}

	if ic.Authenticated {
		username = ic.Username
		password = ic.Password
		password, err = secretFromEnv(password)
		if err != nil {
			return fmt.Errorf("unable to retrieve InfluxDB password from environment: %v", err.Error())
		}
	}

	client, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     url,
		Username: username,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("failed to create InfluxDB client - %v", err.Error())
	}
	s.client = client
	s.badStats = mapset.NewSet[string]()
	return nil
}

func (s *InfluxDBSink) WritePoints(points []Point) error {
	bp, err := client.NewBatchPoints(s.bpConfig)
	if err != nil {
		return fmt.Errorf("unable to create InfluxDB batch points - %v", err.Error())
	}
	for _, point := range points {
		var pts []*client.Point
		for i, f := range point.fields {
			var pt *client.Point
			pt, err = client.NewPoint(point.name, point.tags[i], f, time.Unix(point.time, 0).UTC())
			if err != nil {
				log.Warningf("failed to create point for measurement %q", point.name)
				continue
			}
			pts = append(pts, pt)
		}
		if len(pts) > 0 {
			bp.AddPoints(pts)
		}
	}
	// write the batch
	err = s.client.Write(bp)
	if err != nil {
		return fmt.Errorf("failed to write batch of points - %v", err.Error())
	}
	return nil
}
