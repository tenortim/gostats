package main

import (
	"fmt"
	"log"
	"time"
	"timw/isilon/gostats/papistats"
)

const server = "10.245.108.22"

func main() {
	c := &papistats.Cluster{
		AuthInfo: papistats.AuthInfo{
			Username: "root",
			Password: "a",
		},
		Hostname:  server,
		Port:      8080,
		VerifySSL: false,
	}

	err := c.Authenticate()
	if err != nil {
		log.Fatal(err)
	}

	err = c.GetAPIVersion()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("API Version = %v\n", c.APIVersion)
	stats := []string{
		"cluster.health",
		"node.clientstats.active.nfs",
		//		"node.clientstats.proto.nfs3",
	}
	res, err := c.GetStats(stats)
	if err != nil {
		log.Fatal(err)
	}
	for i, r := range res {
		fmt.Printf("Results for %s\n", stats[i])
		for _, s := range r {
			if s.ErrorString != "" {
				fmt.Printf("Error: %s\n", s.ErrorString)
				continue
			}
			if s.Devid != 0 {
				fmt.Printf("Node %d, ", s.Devid)
			}
			fmt.Printf("Time: %s, Value: %v\n", time.Unix(int64(s.UnixTime), 0), s.Value)
		}
	}
}
