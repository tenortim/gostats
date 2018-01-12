package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// APIVersion is the revision OneFS API
type APIVersion int

// Versions of the OneFS API
const (
	UnknownVersion APIVersion = iota
	OneFS72
	OneFS721
	OneFS80
	OneFS801
	OneFS81
	OneFS811
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
	Hostname   string
	Port       int
	VerifySSL  bool
	APIVersion APIVersion
	baseURL    string
	client     *http.Client
	reauthTime time.Time
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

const authPath = "/session/1/session"
const versionPath = "/platform/1/cluster/config"
const statsPath = "/platform/1/statistics/current"

// Set up Client etc.
func (c *Cluster) initialize() error {
	if c.client != nil {
		return nil
	}
	if c.Username == "" {
		return fmt.Errorf("Authenticate: Username must be set")
	}
	if c.Password == "" {
		return fmt.Errorf("Authenticate: Password must be set")
	}
	if c.Hostname == "" {
		return fmt.Errorf("Authenticate: Hostname must be set")
	}
	if c.Port == 0 {
		c.Port = 8080
	}
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !c.VerifySSL},
	}
	c.client = &http.Client{
		Jar:       jar,
		Transport: tr,
	}
	c.baseURL = "https://" + c.Hostname + ":" + strconv.Itoa(c.Port)
	return nil
}

// Authenticate establishes the initial connection to the cluster
// and uses the provided authentication information to pull and
// store a session cookie
func (c *Cluster) Authenticate() error {
	if err := c.initialize(); err != nil {
		return err
	}
	am := struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Services []string `json:"services"`
	}{
		Username: c.Username,
		Password: c.Password,
		Services: []string{"platform"},
	}
	b, err := json.Marshal(am)
	if err != nil {
		return err
	}
	u, err := url.Parse(c.baseURL + authPath)
	if err != nil {
		return err
	}
	resp, err := c.client.Post(u.String(), "application/json", bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 200(StatusCreated) is success
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Authenticate: auth failed - %s", resp.Status)
	}
	// parse out time limit so we can reauth when necessary
	dec := json.NewDecoder(resp.Body)
	var ar map[string]interface{}
	err = dec.Decode(&ar)
	if err != nil {
		return fmt.Errorf("Authenticate: unable to parse auth response - %s", err)
	}
	var timeout int
	ta, ok := ar["timeout_absolute"]
	if ok {
		timeout = int(ta.(float64))
	} else {
		// This shouldn't happen, but just set it to a sane default
		timeout = 14400
	}
	if timeout > 60 {
		timeout -= 60 // Give a minute's grace to the reauth timer
	}
	c.reauthTime = time.Now().Add(time.Duration(timeout) * time.Second)

	return nil
}

// Handle mapping OneFS version to API version
type osAPImap struct {
	osprefix   string
	apiversion APIVersion
}

var osapivermap = []osAPImap{
	{"v8.1.1.", OneFS811},
	{"v8.1.0.", OneFS81},
	{"v8.0.1.", OneFS801},
	{"v8.0.0.", OneFS80},
	{"v7.2.1.", OneFS721},
	{"v7.2.0.", OneFS72},
}

// GetAPIVersion finds and populates the API version for cluster
func (c *Cluster) GetAPIVersion() error {
	osversion, err := c.GetOSVersion()
	if err != nil {
		return err
	}
	for _, a := range osapivermap {
		if strings.HasPrefix(osversion, a.osprefix) {
			c.APIVersion = a.apiversion
			return nil
		}
	}
	return fmt.Errorf("Unable to find API version matching %s", osversion)
}

// GetOSVersion finds and returns the OneFS version from cluster
func (c *Cluster) GetOSVersion() (string, error) {
	var v interface{}
	resp, err := c.restGet(versionPath)
	if err != nil {
		return "", err
	}
	err = json.Unmarshal(resp, &v)
	if err != nil {
		return "", err
	}
	m := v.(map[string]interface{})
	version := m["onefs_version"]
	r := version.(map[string]interface{})
	release := r["release"]
	rel := release.(string)
	return rel, nil
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
		resp, err := c.restGet(buffer.String())
		if err != nil {
			log.Errorf("failed to get stats: %v\n", err)
			// XXX maybe handle partial errors rather than totally failing?
			return nil, err
		}
		// Debug
		// log.Debugf("stats get response = %s", resp)
		r, err := parseStatResult(resp)
		// XXX -handle error here
		if err != nil {
			log.Errorf("Unable to parse response %s - error %s\n", resp, err)
			return nil, err
		}
		results = append(results, r...)
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

// get REST response from the API
func (c *Cluster) restGet(endpoint string) ([]byte, error) {
	var err error
	if time.Now().After(c.reauthTime) {
		if err = c.Authenticate(); err != nil {
			return nil, err
		}
	}
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Get(u.String())
	if err != nil {
		// XXX handle re-auth here
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	return body, err
}
