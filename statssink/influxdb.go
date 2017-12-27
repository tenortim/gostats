package statssink

import (
	"fmt"
	"log"
	"strconv"
	"time"
	"timw/isilon/gostats/papistats"

	"github.com/influxdata/influxdb/client/v2"
)

// InfluxDBSink defines a structure to talk to an InfluxDB database
type InfluxDBSink struct {
	Cluster  string
	c        client.Client
	bpConfig client.BatchPointsConfig
}

// Init initializes an InfluxDBSink so that points can be written
// The array of argument strings comprises host, port, database
func (s *InfluxDBSink) Init(a []string) error {
	// args are host, port, database
	if len(a) != 3 {
		return fmt.Errorf("InfluxDB Init() wrong number of args %d - expected 3", len(a))
	}
	url := "http://" + a[0] + ":" + a[1]
	s.bpConfig = client.BatchPointsConfig{
		Database:  a[2],
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
func (s *InfluxDBSink) WriteStats(stats []papistats.StatResult) error {
	clusterTags := map[string]string{"cluster": s.Cluster}
	nodeTags := map[string]string{"cluster": s.Cluster}
	bp, err := client.NewBatchPoints(s.bpConfig)
	if err != nil {
		return fmt.Errorf("Unable to create InfluxDB batch points - %v", err.Error())
	}
	for _, stat := range stats {
		var tags map[string]string
		var pts []*client.Point

		// Handle cluster vs node stats
		if stat.Devid == 0 {
			tags = clusterTags
		} else {
			nodeTags["node"] = strconv.Itoa(stat.Devid)
			tags = nodeTags
		}

		switch val := stat.Value.(type) {
		case float64:
			fields := make(map[string]interface{})
			fields["value"] = val
			pt, err := client.NewPoint(stat.Key, tags, fields, time.Unix(stat.UnixTime, 0))
			if err != nil {
				log.Printf("failed to create point %q:%v", stat.Key, stat.Value)
				continue
			}
			pts = append(pts, pt)
		case string:
			fields := make(map[string]interface{})
			fields["value"] = val
			pt, err := client.NewPoint(stat.Key, tags, fields, time.Unix(stat.UnixTime, 0))
			if err != nil {
				log.Printf("failed to create point %q:%v", stat.Key, stat.Value)
				continue
			}
			pts = append(pts, pt)
		case []interface{}:
			for _, vl := range val {
				fields := make(map[string]interface{})
				switch vv := vl.(type) {
				case map[string]interface{}:
					for km, vm := range vv {
						fields[km] = vm
					}
				default:
					fields["value"] = vv
				}
				pt, err := client.NewPoint(stat.Key, tags, fields, time.Unix(stat.UnixTime, 0))
				if err != nil {
					log.Printf("failed to create point %q:%v", stat.Key, stat.Value)
					continue
				}
				pts = append(pts, pt)
			}
		case map[string]interface{}:
			fields := make(map[string]interface{})
			for km, vm := range val {
				fields[km] = vm
			}
			pt, err := client.NewPoint(stat.Key, tags, fields, time.Unix(stat.UnixTime, 0))
			if err != nil {
				log.Printf("failed to create point %q:%v", stat.Key, stat.Value)
				continue
			}
			pts = append(pts, pt)
		default:
			log.Panicf("Failed to handle unwrap of value type %T\n", stat.Value)
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
