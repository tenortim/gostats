package statssink

import "timw/isilon/gostats/papistats"

type config struct {
  addr string
  port int
  database string
  // username and password?
}
