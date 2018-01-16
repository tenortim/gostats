package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MaxAPIPathLen is the limit on the length of an API request URL
const MaxAPIPathLen = 8191

// AuthInfo provides username and password to authenticate
// against the OneFS API
type AuthInfo struct {
	Username string
	Password string
}

// Cluster contains all of the information to talk to a OneFS
// cluster via the OneFS API
type Cluster struct {
	AuthInfo
	Hostname    string
	Port        int
	VerifySSL   bool
	OSVersion   string
	ClusterName string
	baseURL     string
	client      *http.Client
	reauthTime  time.Time
}

// StatResult contains the information returned for a single stat key
// when querying the OneFS statistics API.
// The Value field can be a simple int/float, or it can be a dictionary
// or an array of dictionaries (e.g. protostats results)
type StatResult struct {
	Devid       int         `json:"devid"`
	ErrorString string      `json:"error"`
	ErrorCode   int         `json:"error_code"`
	Key         string      `json:"key"`
	UnixTime    int64       `json:"time"`
	Value       interface{} `json:"value"`
}

const configPath = "/platform/1/cluster/config"
const statsPath = "/platform/1/statistics/current"

// Retry parameter(s) for connection failures
const maxRetries = 8

// Set up Client etc.
func (c *Cluster) initialize() error {
	// already initialized?
	if c.client != nil {
		return nil
	}
	if c.Username == "" {
		return fmt.Errorf("Username must be set")
	}
	if c.Password == "" {
		return fmt.Errorf("Password must be set")
	}
	if c.Hostname == "" {
		return fmt.Errorf("Hostname must be set")
	}
	if c.Port == 0 {
		c.Port = 8080
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !c.VerifySSL},
	}
	c.client = &http.Client{
		Transport: tr,
	}
	c.baseURL = "https://" + c.Hostname + ":" + strconv.Itoa(c.Port)
	return nil
}

// GetClusterConfig pulls information from the cluster config API
// endpoint, including the actual cluster name
func (c *Cluster) GetClusterConfig() error {
	var v interface{}
	resp, err := c.restGet(configPath)
	if err != nil {
		return err
	}
	err = json.Unmarshal(resp, &v)
	if err != nil {
		return err
	}
	m := v.(map[string]interface{})
	version := m["onefs_version"]
	r := version.(map[string]interface{})
	release := r["version"]
	rel := release.(string)
	c.OSVersion = rel
	c.ClusterName = strings.ToLower(m["name"].(string))
	return nil
}

// Connect establishes the initial network connection to the cluster,
// then pulls the cluster config info to get the real cluster name
func (c *Cluster) Connect() error {
	var err error
	if err = c.initialize(); err != nil {
		return err
	}
	if err = c.GetClusterConfig(); err != nil {
		return err
	}
	return nil
}

// GetStats takes an array of statistics keys and returns an
// array of StatResult structures
func (c *Cluster) GetStats(stats []string) ([]StatResult, error) {
	var results []StatResult
	var buffer bytes.Buffer

	initialPath := statsPath + "?degraded=true&devid=all"
	// length of key args
	la := 0
	// Need special case for short last get
	ls := len(stats)
	log.Infof("fetching %d stats from cluster %s", ls, c.ClusterName)
	// max minus (initial string + slop)
	maxlen := MaxAPIPathLen - (len(initialPath) + 100)
	buffer.WriteString(initialPath)
	for i, stat := range stats {
		// 5 == len("?key=")
		if la+5+len(stat) < maxlen {
			buffer.WriteString("&key=")
			buffer.WriteString(stat)
			if i != ls-1 {
				continue
			}
		}
		log.Debugf("cluster %s fetching %s", c.ClusterName, buffer.String())
		resp, err := c.restGet(buffer.String())
		if err != nil {
			log.Errorf("failed to get stats: %v\n", err)
			// XXX maybe handle partial errors rather than totally failing?
			return nil, err
		}
		// XXX - Need to handle return of "errors" here (re-auth)
		log.Debugf("cluster %s got response %s", c.ClusterName, resp)
		// Debug
		// log.Debugf("stats get response = %s", resp)
		r, err := parseStatResult(resp)
		// XXX -handle error here
		if err != nil {
			log.Errorf("Unable to parse response %s - error %s\n", resp, err)
			return nil, err
		}
		log.Debugf("cluster %s parsed stats results = %v", c.ClusterName, r)
		results = append(results, r...)
		buffer.Reset()
	}
	return results, nil
}

func parseStatResult(res []byte) ([]StatResult, error) {
	sa := struct {
		Stats []StatResult `json:"stats"`
	}{}
	err := json.Unmarshal(res, &sa)
	if err != nil {
		return nil, err
	}
	return sa.Stats, nil
}

// helper function
func isConnectionRefused(err error) bool {
	if uerr, ok := err.(*url.Error); ok {
		if nerr, ok := uerr.Err.(*net.OpError); ok {
			if oerr, ok := nerr.Err.(*os.SyscallError); ok {
				if oerr.Err == syscall.ECONNREFUSED {
					return true
				}
			}
		}
	}
	return false
}

// get REST response from the API
func (c *Cluster) restGet(endpoint string) ([]byte, error) {
	var err error
	var resp *http.Response

	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.AuthInfo.Username, c.AuthInfo.Password)
	retrySecs := 1
	for i := 1; i < maxRetries; i++ {
		resp, err = c.client.Do(req)
		if err == nil {
			break
		}
		// XXX - consider adding more retryable cases e.g. temporary DNS hiccup
		if !isConnectionRefused(err) {
			return nil, err
		}
		log.Errorf("Connection to %s refused, retrying in %d seconds", c.Hostname, retrySecs)
		time.Sleep(time.Duration(retrySecs) * time.Second)
		retrySecs *= 2
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	return body, err
}
