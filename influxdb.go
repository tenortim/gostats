package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/influxdata/influxdb/client/v2"
)

// InfluxDBSink defines the data to allow us talk to an InfluxDB database
type InfluxDBSink struct {
	cluster  string
	c        client.Client
	bpConfig client.BatchPointsConfig
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
	var username, password string
	authenticated := false
	// args are host, port, database, and, optionally, username and password
	switch len(args) {
	case 3:
		authenticated = false
	case 5:
		authenticated = true
	default:
		return fmt.Errorf("InfluxDB Init() wrong number of args %d - expected 3", len(args))
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
		fa, ta, err = s.decodeStat(stat)
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
			if drop_stat(&fields) {
				log.Debugf("Cluster %s, dropping broken change_notify stat", s.cluster)
			} else {
				fa = append(fa, fields)
				ta = append(ta, tags)
			}
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
		if drop_stat(&fields) {
			log.Debugf("Cluster %s, dropping broken change_notify stat", s.cluster)
		} else {
			fa = append(fa, fields)
			ta = append(ta, tags)
		}
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

// drop_stat checks the supplied fields and returns a boolean which, if true, specifies that
// this statistic should be dropped.
//
// Some statistics (specifically, SMB change notify) have unusual semantics that can result in
// misleadingly large latency values.
func drop_stat(fields *ptFields) bool {
	if (*fields)["op_name"] == "change_notify" || (*fields)["op_name"] == "read_directory_change" {
		return true
	}
	return false
}
