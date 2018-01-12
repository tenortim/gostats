package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/influxdata/influxdb/client/v2"
)

// InfluxDBSink defines a structure to talk to an InfluxDB database
type InfluxDBSink struct {
	Cluster  string
	c        client.Client
	bpConfig client.BatchPointsConfig
}

// types for the decoded fields and tags
type ptFields map[string]interface{}
type ptTags map[string]string

// Init initializes an InfluxDBSink so that points can be written
// The array of argument strings comprises host, port, database
func (s *InfluxDBSink) Init(args []string) error {
	// args are host, port, database
	if len(args) != 3 {
		return fmt.Errorf("InfluxDB Init() wrong number of args %d - expected 3", len(args))
	}
	host, port, database := args[0], args[1], args[2]
	url := "http://" + host + ":" + port
	s.bpConfig = client.BatchPointsConfig{
		Database:  database,
		Precision: "s",
	}
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr: url,
	})
	if err != nil {
		return fmt.Errorf("Failed to create InfluxDB client - %v", err.Error())
	}
	s.c = c
	return nil
}

// WriteStats takes an array of StatResults and writes them to InfluxDB
func (s *InfluxDBSink) WriteStats(stats []StatResult) error {
	bp, err := client.NewBatchPoints(s.bpConfig)
	if err != nil {
		return fmt.Errorf("Unable to create InfluxDB batch points - %v", err.Error())
	}
	for _, stat := range stats {
		var pts []*client.Point
		var fa []ptFields
		var ta []ptTags
		fa, ta, err = s.decodeStat(stat)
		if err != nil {
			// XXX handle errors
			log.Panicf("Failed to decode stat %+v: %s\n", stat, err)
		}
		for i, f := range fa {
			var pt *client.Point
			pt, err = client.NewPoint(stat.Key, ta[i], f, time.Unix(stat.UnixTime, 0))
			if err != nil {
				log.Warningf("failed to create point %q:%v", stat.Key, stat.Value)
				continue
			}
			pts = append(pts, pt)
		}
		bp.AddPoints(pts)
	}
	// write the batch
	err = s.c.Write(bp)
	if err != nil {
		return fmt.Errorf("Failed to write batch of points - %v", err.Error())
	}
	return nil
}

func (s *InfluxDBSink) decodeStat(stat StatResult) ([]ptFields, []ptTags, error) {
	var tags ptTags
	clusterTags := ptTags{"cluster": s.Cluster}
	nodeTags := ptTags{"cluster": s.Cluster}
	var fa []ptFields
	var ta []ptTags
	// Handle cluster vs node stats
	if stat.Devid == 0 {
		tags = clusterTags
	} else {
		nodeTags["node"] = strconv.Itoa(stat.Devid)
		tags = nodeTags
	}

	switch val := stat.Value.(type) {
	case float64:
		fields := make(ptFields)
		fields["value"] = val
		fa = append(fa, fields)
		ta = append(ta, tags)
	case string:
		fields := make(ptFields)
		fields["value"] = val
		fa = append(fa, fields)
		ta = append(ta, tags)
	case []interface{}:
		for _, vl := range val {
			fields := make(ptFields)
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
								vm = -1
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
		log.Debugf("Unable to decode stat %s due to nil value, skipping", stat.Key)
	default:
		// XXX return error here
		log.Errorf("Unable to decode stat %+v", stat)
		log.Panicf("Failed to handle unwrap of value type %T\n", stat.Value)
	}
	return fa, ta, nil
}
