package statssink

import "timw/isilon/gostats/papistats"

type DBWriter interface {
  // Initialize a statssink 
  Init(a []string) error
  // Write a stat to the sink
  WriteStat(s papistats.StatResult) error
}
