package main

import (
	"fmt"
	"strconv"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// InfluxDBSink defines the data to allow us talk to an InfluxDB database
type InfluxDBSink struct {
	cluster  string
	c        influxdb2.Client
	writeAPI api.WriteAPI
}

// types for the decoded fields and tags
type ptFields map[string]interface{}
type ptTags map[string]string

// GetInfluxDBWriter returns an InfluxDB DBWriter
func GetInfluxDBWriter() DBWriter {
	return &InfluxDBSink{}
}

// Init initializes an InfluxDBSink so that points can be written
// The array of argument strings comprises host, port, database
func (s *InfluxDBSink) Init(cluster string, args []string) error {
	// args are host, port, database, and access token
	if len(args) != 5 {
		return fmt.Errorf("InfluxDB Init() wrong number of args %d - expected 4", len(args))
	}

	s.cluster = cluster
	host, port, bucket, org, token := args[0], args[1], args[2], args[3], args[4]

	url := "http://" + host + ":" + port
	client := influxdb2.NewClient(url, token)
	writeAPI := client.WriteAPI(org, bucket)

	//if err != nil {
	//	return fmt.Errorf("failed to create InfluxDB client - %v", err.Error())
	//}
	s.c = client
	s.writeAPI = writeAPI

	// Get errors channel
	errorsCh := writeAPI.Errors()
	// Create go proc for reading and logging errors
	go func() {
		for err := range errorsCh {
			log.Errorf("InfluxDB async write error for cluster %s: %s\n", cluster, err.Error())
		}
	}()
	return nil
}

// WriteStats takes an array of StatResults and writes them to InfluxDB
func (s *InfluxDBSink) WriteStats(stats []StatResult) error {
	for _, stat := range stats {
		var fa []ptFields
		var ta []ptTags
		var err error
		fa, ta, err = s.decodeStat(stat)
		if err != nil {
			// TODO consider trying to recover/handle errors
			log.Panicf("Failed to decode stat %+v: %s\n", stat, err)
		}
		for i, f := range fa {
			pt := influxdb2.NewPoint(stat.Key, ta[i], f, time.Unix(stat.UnixTime, 0).UTC())
			s.writeAPI.WritePoint(pt)
		}

	}
	// write the batch
	s.writeAPI.Flush()
	return nil
}

// helper function
func ptmapCopy(tags ptTags) ptTags {
	copy := ptTags{}
	for k, v := range tags {
		copy[k] = v
	}
	return copy
}

func (s *InfluxDBSink) decodeStat(stat StatResult) ([]ptFields, []ptTags, error) {
	var baseTags ptTags
	clusterTags := ptTags{"cluster": s.cluster}
	nodeTags := ptTags{"cluster": s.cluster}
	var fa []ptFields
	var ta []ptTags
	// Handle cluster vs node stats
	if stat.Devid == 0 {
		baseTags = clusterTags
	} else {
		nodeTags["node"] = strconv.Itoa(stat.Devid)
		baseTags = nodeTags
	}

	switch val := stat.Value.(type) {
	case float64:
		fields := make(ptFields)
		fields["value"] = val
		fa = append(fa, fields)
		ta = append(ta, baseTags)
	case string:
		fields := make(ptFields)
		fields["value"] = val
		fa = append(fa, fields)
		ta = append(ta, baseTags)
	case []interface{}:
		for _, vl := range val {
			fields := make(ptFields)
			tags := ptmapCopy(baseTags)
			switch vv := vl.(type) {
			case map[string]interface{}:
				for km, vm := range vv {
					// op_name, class_name are tags(indexed), not fields
					if km == "op_name" || km == "class_name" {
						tags[km] = vm.(string)
					} else {
						// Ugly code to fix broken unsigned op_id from the API
						if km == "op_id" {
							if vm.(float64) == (2 ^ 32 - 1) {
								vm = float64(-1)
							}
						}
						fields[km] = vm
					}
				}
			default:
				fields["value"] = vv
			}
			fa = append(fa, fields)
			ta = append(ta, tags)
		}
	case map[string]interface{}:
		fields := make(ptFields)
		tags := ptmapCopy(baseTags)
		for km, vm := range val {
			// op_name, class_name are tags(indexed), not fields
			if km == "op_name" || km == "class_name" {
				tags[km] = vm.(string)
			} else {
				// Ugly code to fix broken unsigned op_id from the API
				if km == "op_id" {
					if vm.(float64) == (2 ^ 32 - 1) {
						// JSON numbers are floats (in Javascript)
						// cast so that InfluxDB doesn't get upset with the mismatch
						vm = float64(-1)
					}
				}
				fields[km] = vm
			}
		}
		fa = append(fa, fields)
		ta = append(ta, tags)
	case nil:
		// It seems that the stats API can return nil values where
		// ErrorString is set, but ErrorCode is 0
		// Drop these, but log them if log level is high enough
		log.Debugf("Cluster %s, unable to decode stat %s due to nil value, skipping", s.cluster, stat.Key)
	default:
		// TODO consider returning an error rather than panicing
		log.Errorf("Unable to decode stat %+v", stat)
		log.Panicf("Failed to handle unwrap of value type %T\n", stat.Value)
	}
	return fa, ta, nil
}
