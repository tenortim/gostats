package main

import (
	"strconv"
)

// types for the decoded fields and tags
type ptFields map[string]interface{}
type ptTags map[string]string

// helper function
func ptTagmapCopy(tags ptTags) ptTags {
	copy := ptTags{}
	for k, v := range tags {
		copy[k] = v
	}
	return copy
}

// DecodeStat takes the JSON result from the OneFS statistics API and breaks it
// out into fields and tags usable by the back end writers
func DecodeStat(cluster string, stat StatResult) ([]ptFields, []ptTags, error) {
	var baseTags ptTags
	clusterStatTags := ptTags{"cluster": cluster}
	nodeStatTags := ptTags{"cluster": cluster}
	var fa []ptFields
	var ta []ptTags
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
			tags := ptTagmapCopy(baseTags)
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
			if isInvalidStat(&fields) {
				log.Debugf("Cluster %s, dropping broken change_notify stat", cluster)
			} else {
				fa = append(fa, fields)
				ta = append(ta, tags)
			}
		}
	case map[string]interface{}:
		fields := make(ptFields)
		tags := ptTagmapCopy(baseTags)
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
		if isInvalidStat(&fields) {
			log.Debugf("Cluster %s, dropping broken change_notify stat", cluster)
		} else {
			fa = append(fa, fields)
			ta = append(ta, tags)
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
	return fa, ta, nil
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
