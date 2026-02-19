package main

import (
	"context"
	"fmt"
	"log/slog"
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
	
	// ping the database to ensure we can connect
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ok, err := client.Ping(ctx)
	if err != nil {
		return fmt.Errorf("failed to ping InfluxDBv2 - %v", err.Error())
	}
	if !ok {
		return fmt.Errorf("InfluxDBv2 ping failed - server not reachable")
	}
	log.Info("successfully connected to InfluxDBv2", slog.String("cluster", cluster))
	
	writeAPI := client.WriteAPI(ic.Org, ic.Bucket)
	s.c = client
	s.writeAPI = writeAPI

	// Get errors channel
	errorsCh := writeAPI.Errors()
	// Create goroutine for reading and logging errors
	go func() {
		for err := range errorsCh {
			log.Error("InfluxDB async write failed", slog.String("cluster", cluster), slog.String("error", err.Error()))
		}
	}()
	s.badStats = mapset.NewSet[string]()
	return nil
}

// WritePoints writes a batch of points to InfluxDBv2
func (s *InfluxDBv2Sink) WritePoints(points []Point) error {
	for _, point := range points {
		for i, field := range point.fields {
			pt := influxdb2.NewPoint(point.name, point.tags[i], field, time.Unix(point.time, 0).UTC())
			s.writeAPI.WritePoint(pt)
		}
	}
	// write the batch
	s.writeAPI.Flush()
	return nil
}
