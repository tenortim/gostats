package main

import (
	"fmt"
	"strconv"
)

// stats returned from OneFS can be "multi-valued" i.e.,
// a single stat can return values for min, max and avg of
// several measures such as op rate or latency

// Point represents a single named measurement at a given time in a timeseries data set.
// Because some OneFS statistics return multiple sets of data with unique combinations
// of tags, there is a single measurement name, and timestamp, but an array of
// field names/values, and an array of tag names/values.
type Point struct {
	name   string
	time   int64
	fields []ptFields
	tags   []ptTags
}

// ptFields maps the fields for a given instance of a metric to their values
type ptFields map[string]any

// ptTags maps the tags for a given instance of a metric to their values
type ptTags map[string]string

// ptTagmapCopy makes a copy of the given tag map.
// When a metric yields an array of points, each point needs its own distinct set of tags
func ptTagmapCopy(tags ptTags) ptTags {
	copy := ptTags{}
	for k, v := range tags {
		copy[k] = v
	}
	return copy
}

// DecodeStat takes the JSON result from the OneFS statistics API and breaks it
// out into fields and tags usable by the back end writers.
func DecodeStat(cluster string, stat StatResult) ([]ptFields, []ptTags, error) {
	var baseTags ptTags
	clusterStatTags := ptTags{"cluster": cluster}
	nodeStatTags := ptTags{"cluster": cluster}
	var mfa []ptFields // metric field array i.e., array of field to value mappings for each unique tag set for this metric
	var mta []ptTags   // metric tag array i.e., array of tag name to tag value mappings for each unique tag set for this metric

	// Handle cluster vs node stats
	if stat.Devid == 0 {
		baseTags = clusterStatTags
	} else {
		nodeStatTags["node"] = strconv.Itoa(stat.Devid)
		baseTags = nodeStatTags
	}

	switch val := stat.Value.(type) {
	case float64:
		fields := make(ptFields)
		fields["value"] = val
		mfa = append(mfa, fields)
		mta = append(mta, baseTags)
	case string:
		// This should not happen, and if it does, we won't have a usable value to push to the database
		log.Warningf("stat %s only has single (unusable) string value", stat.Key)
		fields := make(ptFields)
		fields["value"] = val
		mfa = append(mfa, fields)
		mta = append(mta, baseTags)
	case []any:
		// handle stats that return an array of "values" with distinct tag sets e.g., protostats
		for _, vl := range val {
			fields := make(ptFields)
			tags := ptTagmapCopy(baseTags)
			switch vv := vl.(type) {
			case map[string]any:
				for km, vm := range vv {
					// values of type string, e.g. op_name are converted to tags
					switch vm := vm.(type) {
					case string:
						tags[km] = vm
					default:
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
			if isInvalidStat(&fields) {
				log.Debugf("Cluster %s, dropping broken change_notify stat", cluster)
			} else {
				mfa = append(mfa, fields)
				mta = append(mta, tags)
			}
		}
	case map[string]any:
		fields := make(ptFields)
		tags := ptTagmapCopy(baseTags)
		for km, vm := range val {
			// values of type string, e.g. op_name are converted to tags
			switch vm := vm.(type) {
			case string:
				tags[km] = vm
			default:
				// Ugly code to fix broken unsigned op_id from the API
				if km == "op_id" {
					if vm.(float64) == (2 ^ 32 - 1) {
						vm = float64(-1)
					}
				}
				fields[km] = vm
			}
		}
		if isInvalidStat(&fields) {
			log.Debugf("Cluster %s, dropping broken change_notify stat", cluster)
		} else {
			mfa = append(mfa, fields)
			mta = append(mta, tags)
		}
	case nil:
		// It seems that the stats API can return nil values where
		// ErrorString is set, but ErrorCode is 0
		// Drop these, but log them if log level is high enough
		log.Debugf("Cluster %s, unable to decode stat %s due to nil value, skipping", cluster, stat.Key)
	default:
		// TODO consider returning an error rather than panicing
		log.Errorf("Unable to decode stat %+v", stat)
		log.Panicf("Failed to handle unwrap of value type %T\n", stat.Value)
	}
	return mfa, mta, nil
}

// isInvalidStat checks the supplied fields and returns a boolean which, if true, specifies that
// this statistic should be dropped.
//
// Some statistics (specifically, SMB change notify) have unusual semantics that can result in
// misleadingly large latency values.
func isInvalidStat(fields *ptFields) bool {
	if (*fields)["op_name"] == "change_notify" || (*fields)["op_name"] == "read_directory_change" {
		return true
	}
	return false
}

// WriteStats takes an array of StatResults and writes them to the requested backend database
func (c *Cluster) WriteStats(ss DBWriter, stats []StatResult) error {
	points := make([]Point, 0, len(stats)) // try to preallocate at least some space here
	for _, stat := range stats {
		if stat.ErrorCode != 0 {
			if !c.badStats.Contains(stat.Key) {
				log.Warningf("Unable to retrieve stat %v from cluster %v, error %v", stat.Key, c.ClusterName, stat.ErrorString)
			}
			// add it to the set of bad (unavailable) stats
			c.badStats.Add(stat.Key)
			continue
		}
		fa, ta, err := DecodeStat(c.ClusterName, stat)
		if err != nil {
			// TODO consider trying to recover/handle errors
			log.Panicf("Failed to decode stat %+v: %s\n", stat, err)
		}
		point := Point{name: stat.Key, time: stat.UnixTime, fields: fa, tags: ta}
		points = append(points, point)
	}
	// write the points to the database
	err := ss.WritePoints(points)
	if err != nil {
		return fmt.Errorf("failed to write batch of points - %v", err.Error())
	}
	return nil
}
