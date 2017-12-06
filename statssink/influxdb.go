package statssink

import (
  "log"
  "strconv"
  "timw/isilon/gostats/papistats"
  "github.com/influxdata/influxdb/client/v2"
)

type InfluxDBSink struct {
  cluster string
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
  cluster_tags := map[string]string{"cluster": s.cluster}
  node_tags := map[string]string{"cluster": s.cluster}
  bp, err := client.NewBatchPoints(s.bpConfig)
  if err != nil {
    return fmt.Errof("Unable to create InfluxDB batch points - %v", err.Error())
  }
  for _, stat := range stats {
    if stat.Devid = 0 {
      tags := cluster_tags
    } else {
      node_tags["node"] = strconv.Itoa(stat.Devid)
    }
    fields := map[string]interface{}{
      "value": stat.Value,
    }
    pt, err := s.c.NewPoint(stat.Key, tags, fields, stat.UnixTime)
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
