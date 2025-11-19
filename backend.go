package main

import (
	"fmt"
	"maps"
	"strconv"
	"time"
)

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

// DecodeProtocolSummaryStat takes a SummaryStatsProtocolItem and decodes it into
// fields and tags usable by the back end writers.
func DecodeProtocolSummaryStat(cluster string, pss SummaryStatsProtocolItem) (ptFields, ptTags) {
	tags := ptTags{"cluster": cluster}
	fields := make(ptFields)
	if pss.Node != nil {
		tags["node"] = strconv.FormatInt(*pss.Node, 10)
	}
	tags["class"] = pss.Class
	tags["operation"] = pss.Operation
	tags["protocol"] = pss.Protocol
	fields["in"] = pss.In
	fields["in_avg"] = pss.InAvg
	fields["in_max"] = pss.InMax
	fields["in_min"] = pss.InMin
	fields["in_standard_dev"] = pss.InStandardDev
	fields["operation_count"] = pss.OperationCount
	fields["operation_rate"] = pss.OperationRate
	fields["out"] = pss.Out
	fields["out_avg"] = pss.OutAvg
	fields["out_max"] = pss.OutMax
	fields["out_min"] = pss.OutMin
	fields["out_standard_dev"] = pss.OutStandardDev
	fields["time"] = pss.Time
	fields["time_avg"] = pss.TimeAvg
	fields["time_max"] = pss.TimeMax
	fields["time_min"] = pss.TimeMin
	fields["time_standard_dev"] = pss.TimeStandardDev
	return fields, tags
}

// DecodeClientSummaryStat takes a SummaryStatsClientItem and decodes it into
// fields and tags usable by the back end writers.
func DecodeClientSummaryStat(cluster string, css SummaryStatsClientItem) (ptFields, ptTags) {
	tags := ptTags{"cluster": cluster}
	fields := make(ptFields)
	if css.Node != nil {
		tags["node"] = strconv.FormatInt(*css.Node, 10)
	}
	tags["class"] = css.Class
	fields["in"] = css.In
	fields["in_avg"] = css.InAvg
	fields["in_max"] = css.InMax
	fields["in_min"] = css.InMin
	tags["local_addr"] = css.LocalAddr
	tags["local_name"] = css.LocalName
	fields["num_operations"] = css.NumOperations
	fields["operation_rate"] = css.OperationRate
	tags["protocol"] = css.Protocol
	fields["out"] = css.Out
	fields["out_avg"] = css.OutAvg
	fields["out_max"] = css.OutMax
	fields["out_min"] = css.OutMin
	tags["remote_addr"] = css.RemoteAddr
	tags["remote_name"] = css.RemoteName
	fields["time"] = css.Time
	fields["time_avg"] = css.TimeAvg
	fields["time_max"] = css.TimeMax
	fields["time_min"] = css.TimeMin
	if css.User != nil {
		tags["user_id"] = css.User.ID
		tags["user_name"] = css.User.Name
		tags["user_type"] = css.User.Type
	}
	return fields, tags
}

// DecodeStat takes the JSON result from the OneFS statistics API and breaks it
// out into fields and tags usable by the back end writers.
func DecodeStat(cluster string, stat StatResult, degraded bool) ([]ptFields, []ptTags, error) {
	var initialTags ptTags
	clusterStatTags := ptTags{"cluster": cluster, "degraded": strconv.FormatBool(degraded)}
	nodeStatTags := ptTags{"cluster": cluster, "degraded": strconv.FormatBool(degraded)}
	var mfa []ptFields // metric field array i.e., array of field to value mappings for each unique tag set for this metric
	var mta []ptTags   // metric tag array i.e., array of tag name to tag value mappings for each unique tag set for this metric

	// Handle cluster vs node stats
	if stat.Devid == 0 {
		initialTags = clusterStatTags
	} else {
		nodeStatTags["devid"] = strconv.Itoa(stat.Devid)
		if stat.Node != nil {
			nodeStatTags["node"] = strconv.Itoa(*stat.Node)
		} else {
			// Should not happen, but fall back to using devid as node tag
			nodeStatTags["node"] = nodeStatTags["devid"]
		}
		initialTags = nodeStatTags
	}
	mfa, mta, err := decodeValue(stat.Key, "value", stat.Value, initialTags, 0)
	if err != nil {
		return nil, nil, err
	}
	return mfa, mta, nil
}

// decodeValue recursively parses that stat value and flattens the result into an array of fields and tags
// A few assertions:
// 1. We will never see a directly nested array
// 2. Primitive values (float64, int64, int, string) will only be seen at depth 0
// 3. We will never see a string value (tag) at depth 0
func decodeValue(statname string, fieldname string, v any, baseTags ptTags, depth int) ([]ptFields, []ptTags, error) {
	var mfa []ptFields // metric field array i.e., array of field to value mappings for each unique tag set for this metric
	var mta []ptTags   // metric tag array i.e., array of tag name to tag value mappings for each unique tag set for this metric

	log.Debugf("decodeValue: stat=%s, field=%s, value=%#v, depth=%d", statname, fieldname, v, depth)
	switch val := v.(type) {
	case float64, int64, int:
		log.Debugf("decoding primitive value: %T", val)
		if fieldname == "" {
			// We should never get here, as we should have handled this in the parent call
			log.Panicf("unexpected primitive value with no name in stat %s", statname)
		}
		fields := make(ptFields)
		fields[fieldname] = val
		log.Debugf("decoded fields: %#v", fields)
		mfa = append(mfa, fields)
		mta = append(mta, baseTags)
	case string:
		if depth == 0 {
			// This should not happen, and if it does, we won't have a usable value to push to the database
			return nil, nil, fmt.Errorf("stat %s only has single (unusable) string value", statname)
		}
		tags := maps.Clone(baseTags)
		tags[fieldname] = val
		log.Debugf("decoding tag value: %s=%s", fieldname, val)
		mta = append(mta, tags)
	case []any:
		// handle stats that return an array of "values" with distinct tag sets e.g., protostats
		log.Debugf("decoding array of %d values", len(val))
		for _, vl := range val {
			nfa, nta, err := decodeValue(statname, "", vl, baseTags, depth+1)
			if err != nil {
				log.Errorf("Failed to decode stat %s: %s", statname, err)
				return nil, nil, err
			}
			log.Debugf("decoded array element to %d fields and %d tags", len(nfa), len(nta))
			mfa = append(mfa, nfa...)
			mta = append(mta, nta...)
		}
		return mfa, mta, nil
	case map[string]any:
		log.Debugf("decoding map with %d keys", len(val))
		fields := make(ptFields)
		tags := make(ptTags)
		maps.Copy(tags, baseTags)
		subfields := make([]ptFields, 0)
		subtags := make([]ptTags, 0)
		// is this a simple map with no sub-arrays?
		simple := true
		for km, vm := range val {
			log.Debugf("decoding map key %s", km)
			_, isarray := vm.([]any)
			nfa, nta, err := decodeValue(statname, km, vm, baseTags, depth+1)
			log.Debugf("decoded map key %s to fields %#v and tags %#v", km, nfa, nta)
			if err != nil {
				log.Errorf("Failed to decode stat %s: %s", statname, err)
				return nil, nil, err
			}
			if len(nfa) == 0 {
				// expected for tag values in a map
				maps.Copy(tags, nta[0])
			} else if len(nfa) == 1 && !isarray {
				// We have a single primitive value, so add it to the base fields
				maps.Copy(fields, nfa[0])
			} else if isarray {
				// We have multiple sub-values, so we need to merge the base fields and tags into each of them
				simple = false
				subfields = append(subfields, nfa...)
				subtags = append(subtags, nta...)
			} else {
				// This should not happen
				log.Panicf("unexpected multiple field values in map key %s of stat %s", km, statname)
			}
		}
		if simple {
			// We had a simple map with no sub-arrays, so just return the single set of fields and tags
			log.Debugf("decoded simple map to fields: %#v and tags: %#v", fields, tags)
			if isInvalidStat(&tags) {
				log.Debugf("Cluster %s, dropping broken change_notify stat", baseTags["cluster"])
			} else {
				mfa = append(mfa, fields)
				mta = append(mta, tags)
			}
		} else {
			// We had a sub-array, so we need to combine the base fields and tags with each of the sub ones
			log.Debugf("decoded complex map to %d sub-fields and %d sub-tags", len(subfields), len(subtags))
			for i := range subfields {
				var f ptFields
				var t ptTags
				f = maps.Clone(fields)
				t = maps.Clone(tags)
				// merge the base fields and tags into the sub ones
				maps.Copy(f, subfields[i])
				maps.Copy(t, subtags[i])
				if isInvalidStat(&t) {
					log.Debugf("Cluster %s, dropping broken change_notify stat", baseTags["cluster"])
				} else {
					mfa = append(mfa, f)
					mta = append(mta, t)
				}
			}
		}
	default:
		// TODO consider returning an error rather than panicing
		log.Errorf("Unable to decode stat %s", statname)
		log.Panicf("Failed to handle unwrap of value type %T in stat %s\n", val, statname)
	}
	log.Debugf("decodeValue returning %d sets of fields and %d sets of tags", len(mfa), len(mta))
	return mfa, mta, nil
}

// isInvalidStat checks the supplied tags and returns a boolean which, if true, specifies that
// this statistic should be dropped.
//
// Some statistics (specifically, SMB change notify) have unusual semantics that can result in
// misleadingly large latency values.
func isInvalidStat(tags *ptTags) bool {
	if (*tags)["op_name"] == "change_notify" || (*tags)["op_name"] == "read_directory_change" {
		return true
	}
	return false
}

// WriteStats takes an array of StatResults and writes them to the requested backend database
func (c *Cluster) WriteStats(gc globalConfig, ss DBWriter, stats []StatResult) error {
	points := make([]Point, 0, len(stats)) // try to preallocate at least some space here
	for _, stat := range stats {
		degraded := false
		switch stat.ErrorCode {
		case StatErrorNone:
			// all good
		case StatErrorDegraded:
			// degraded result
			degraded = true
			log.Debugf("Stat %v from cluster %v returned degraded result", stat.Key, c.ClusterName)
		case StatErrorNotPresent, StatErrorNotImplemented, StatErrorNotConfigured, StatErrorNoData:
			// skip stats that returned an error
			if !c.badStats.Contains(stat.Key) {
				log.Warningf("Unable to retrieve stat %v from cluster %v, error %v", stat.Key, c.ClusterName, stat.ErrorString)
			}
			// add it to the set of bad (unavailable) stats
			c.badStats.Add(stat.Key)
			continue
		case StatErrorStale, StatErrorConnTimeout, StatErrorNoHistory, StatErrorSystem:
			// just skip over this time
			log.Warningf("Skipping over stat %v from cluster %v due to error %v", stat.Key, c.ClusterName, stat.ErrorString)
			continue
		default:
			// unknown error
			log.Errorf("Stat %v from cluster %v returned unknown error code %v (%v)", stat.Key, c.ClusterName, stat.ErrorCode, stat.ErrorString)
			continue
		}
		fa, ta, err := DecodeStat(c.ClusterName, stat, degraded)
		if err != nil {
			// TODO consider trying to recover/handle errors
			log.Panicf("Failed to decode stat %+v: %s\n", stat, err)
		}
		point := Point{name: stat.Key, time: stat.UnixTime, fields: fa, tags: ta}
		points = append(points, point)
	}
	// write the points to the database, retrying up to the limit
	const maxRetryTime = time.Second * 1280
	retryTime := time.Second * time.Duration(gc.ProcessorRetryIntvl)
	var err error
	for i := 1; i <= gc.ProcessorMaxRetries; i++ {
		err = ss.WritePoints(points)
		if err == nil {
			break
		}
		log.Errorf("failed writing to back end database: %v - retry #%d in %v", err, i, retryTime)
		time.Sleep(retryTime)
		if retryTime < maxRetryTime {
			retryTime *= 2
		}
	}
	if err != nil {
		log.Errorf("ProcessorMaxRetries exceeded, failed to write stats to database: %s", err)
		return err
	}
	return nil
}
