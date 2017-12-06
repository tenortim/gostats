package statssink

import (
  "fmt"
  "log"
  "strconv"
  "time"
  "timw/isilon/gostats/papistats"
  "github.com/influxdata/influxdb/client/v2"
)

type InfluxDBSink struct {
  Cluster string
  c client.Client
  bpConfig client.BatchPointsConfig
}

func (s *InfluxDBSink) Init(a []string) error {
  // args are host, port, database
  if len(a) != 3 {
    return fmt.Errorf("InfluxDB Init() wrong number of args %d - expected 3\n", len(a))
  }
  url := "http://" + a[0] + ":" + a[1]
  s.bpConfig = client.BatchPointsConfig{
    Database: a[2],
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

func (s *InfluxDBSink) WriteStats(stats []papistats.StatResult) error {
  cluster_tags := map[string]string{"cluster": s.Cluster}
  node_tags := map[string]string{"cluster": s.Cluster}
  bp, err := client.NewBatchPoints(s.bpConfig)
  if err != nil {
    return fmt.Errorf("Unable to create InfluxDB batch points - %v", err.Error())
  }
  for _, stat := range stats {
    var tags map[string]string
    // Handle cluster vs node stats
    if stat.Devid == 0 {
      tags = cluster_tags
    } else {
      node_tags["node"] = strconv.Itoa(stat.Devid)
      tags = node_tags
    }
    fields := map[string]interface{}{
      "value": stat.Value,
    }
    pt, err := client.NewPoint(stat.Key, tags, fields, time.Unix(stat.UnixTime, 0))
    if err != nil {
      log.Printf("failed to create point %q:%v", stat.Key, stat.Value)
      continue
    }
    bp.AddPoint(pt)
  }
  // write the batch
  err = s.c.Write(bp)
  if err != nil {
    return fmt.Errorf("Failed to write batch of points - %v", err.Error())
  }
  return nil
}
