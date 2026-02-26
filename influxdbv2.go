package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

// InfluxDBv2Sink defines the data to allow us talk to an InfluxDBv2 database
type InfluxDBv2Sink struct {
	cluster  string
	c        influxdb2.Client
	writeAPI api.WriteAPIBlocking
}

// GetInfluxDBv2Writer returns an InfluxDBv2 DBWriter
func GetInfluxDBv2Writer() DBWriter {
	return &InfluxDBv2Sink{}
}

// Init initializes an InfluxDBv2Sink so that points can be written
func (s *InfluxDBv2Sink) Init(ctx context.Context, cluster string, config *tomlConfig, _ int, _ map[string]statDetail) error {
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
		return fmt.Errorf("unable to retrieve InfluxDBv2 token from environment: %w", err)
	}
	client := influxdb2.NewClient(url, token)
	
	// ping the database to ensure we can connect
	pingCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ok, err := client.Ping(pingCtx)
	if err != nil {
		return fmt.Errorf("failed to ping InfluxDBv2: %w", err)
	}
	if !ok {
		return fmt.Errorf("InfluxDBv2 ping failed - server not reachable")
	}
	log.Info("successfully connected to InfluxDBv2", slog.String("cluster", cluster))
	
	s.c = client
	s.writeAPI = client.WriteAPIBlocking(ic.Org, ic.Bucket)
	return nil
}

// WritePoints writes a batch of points to InfluxDBv2
func (s *InfluxDBv2Sink) WritePoints(ctx context.Context, points []Point) error {
	var pts []*write.Point
	for _, point := range points {
		for i, field := range point.fields {
			pts = append(pts, influxdb2.NewPoint(point.name, point.tags[i], field, time.Unix(point.time, 0).UTC()))
		}
	}
	if err := s.writeAPI.WritePoint(ctx, pts...); err != nil {
		return fmt.Errorf("InfluxDBv2 write failed: %w", err)
	}
	return nil
}
