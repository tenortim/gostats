package statssink

import "timw/isilon/gostats/papistats"

type DBWriter interface {
  // Initialize a statssink
  Init(a []string) error
  // Write a stat to the sink
  WriteStats(stats []papistats.StatResult) error
}
