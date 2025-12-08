package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"golang.org/x/net/publicsuffix"
)

// MaxAPIPathLen is the limit on the length of an API request URL
const MaxAPIPathLen = 8198

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
	AuthType     string
	Hostname     string
	Port         int
	VerifySSL    bool
	OSVersion    string
	ClusterName  string
	baseURL      string
	client       *http.Client
	csrfToken    string
	reauthTime   time.Time
	maxRetries   int
	PreserveCase bool
	badStats     mapset.Set[string]
}

// StatResult contains the information returned for a single stat key
// when querying the OneFS statistics API.
// The Value field can be a simple int/float, or it can be a dictionary
// or an array of dictionaries (e.g. protostats results), or even more complex
// nested structures.
type StatResult struct {
	Devid       int    `json:"devid"`
	Node        *int   `json:"node,omitempty"`
	ErrorString string `json:"error"`
	ErrorCode   int    `json:"error_code"`
	Key         string `json:"key"`
	UnixTime    int64  `json:"time"`
	Value       any    `json:"value"`
}

// statDetail holds the metadata information for a stat as retrieved from
// the statistics '/keys' endpoint
type statDetail struct {
	//	key         string
	valid       bool // flag if this stat doesn't exist on this cluster
	description string
	units       string
	scope       string
	datatype    string // JSON "type"
	aggType     string // aggregation type - add enum if/when we use it
	updateIntvl float64
}

// API endpoint paths
const sessionPath = "/session/1/session"
const configPath = "/platform/1/cluster/config"
const statsPath = "/platform/1/statistics/current"
const statInfoPath = "/platform/1/statistics/keys/"
const summaryStatsPath = "/platform/3/statistics/summary/"

// Summary stats will be persisted as "node.summary.<stat_type>"
const summaryStatsBasename = "node.summary."

// Isi stats key error codes
const (
	StatErrorNone = iota
	StatErrorNotPresent
	StatErrorNotImplemented
	StatErrorDegraded
	StatErrorStale
	StatErrorConnTimeout
	StatErrorNoHistory
	StatErrorSystem
	StatErrorNotConfigured
	StatErrorNoData
)

const maxTimeoutSecs = 1800 // clamp retry timeout to 30 minutes

// SummaryStatsProtocol stores the return from the /3/statistics/summary/statistics endpoint
// which returns an array of protocol summary stats or an array of errors
type SummaryStatsProtocol struct {
	// A list of errors that may be returned.
	Errors []ApiError `json:"errors,omitempty"`
	// or the array of summary stats
	Protocol []SummaryStatsProtocolItem `json:"protocol,omitempty"`
}

// An object describing a single error.
type ApiError struct {
	Code    string  `json:"code"`            // The error code.
	Field   *string `json:"field,omitempty"` // The field with the error if applicable.
	Message string  `json:"message"`         // The error message.

}

// SummaryStatsProtocolItem describes a single protocol summary stat entry
type SummaryStatsProtocolItem struct {
	Class           string  `json:"class"`             // The class of the operation.
	In              float64 `json:"in"`                // Rate of input (in bytes/second) for an operation since the last time isi statistics collected the data.
	InAvg           float64 `json:"in_avg"`            // Average input (received) bytes for an operation, in bytes.
	InMax           float64 `json:"in_max"`            // Maximum input (received) bytes for an operation, in bytes.
	InMin           float64 `json:"in_min"`            // Minimum input (received) bytes for an operation, in bytes.
	InStandardDev   float64 `json:"in_standard_dev"`   // Standard deviation for input (received) bytes for an operation, in bytes.
	Node            *int64  `json:"node"`              // The node on which the operation was performed.
	Operation       string  `json:"operation"`         // The operation performed.
	OperationCount  int64   `json:"operation_count"`   // The number of times an operation has been performed.
	OperationRate   float64 `json:"operation_rate"`    // The rate (in ops/second) at which an operation has been performed.
	Out             float64 `json:"out"`               // Rate of output (in bytes/second) for an operation since the last time isi statistics collected the data.
	OutAvg          float64 `json:"out_avg"`           // Average output (sent) bytes for an operation, in bytes.
	OutMax          float64 `json:"out_max"`           // Maximum output (sent) bytes for an operation, in bytes.
	OutMin          float64 `json:"out_min"`           // Minimum output (sent) bytes for an operation, in bytes.
	OutStandardDev  float64 `json:"out_standard_dev"`  // Standard deviation for output (received) bytes for an operation, in bytes.
	Protocol        string  `json:"protocol"`          // The protocol of the operation.
	Time            int64   `json:"time"`              // Unix Epoch time in seconds of the request.
	TimeAvg         float64 `json:"time_avg"`          // The average elapsed time (in microseconds) taken to complete an operation.
	TimeMax         float64 `json:"time_max"`          // The maximum elapsed time (in microseconds) taken to complete an operation.
	TimeMin         float64 `json:"time_min"`          // The minimum elapsed time (in microseconds) taken to complete an operation.
	TimeStandardDev float64 `json:"time_standard_dev"` // The standard deviation time (in microseconds) taken to complete an operation.
}

// SummaryStatsClient stores the return from the /3/statistics/summary/client endpoint
// which returns an array of client summary stats or an array of errors
type SummaryStatsClient struct {
	// A list of errors that may be returned.
	Errors []ApiError `json:"errors,omitempty"`
	// or the array of summary stats
	Client []SummaryStatsClientItem `json:"client,omitempty"`
}

// SummaryStatsClientItem describes a single client summary stat entry
type SummaryStatsClientItem struct {
	Class         string  `json:"class"`
	In            float64 `json:"in"`
	InAvg         float64 `json:"in_avg"`
	InMax         float64 `json:"in_max"`
	InMin         float64 `json:"in_min"`
	LocalAddr     string  `json:"local_addr"`
	LocalName     string  `json:"local_name"`
	Node          *int64  `json:"node"`
	NumOperations int64   `json:"num_operations"`
	OperationRate float64 `json:"operation_rate"`
	Out           float64 `json:"out"`
	OutAvg        float64 `json:"out_avg"`
	OutMax        float64 `json:"out_max"`
	OutMin        float64 `json:"out_min"`
	Protocol      string  `json:"protocol"`
	RemoteAddr    string  `json:"remote_addr"`
	RemoteName    string  `json:"remote_name"`
	Time          int64   `json:"time"`
	TimeAvg       float64 `json:"time_avg"`
	TimeMax       float64 `json:"time_max"`
	TimeMin       float64 `json:"time_min"`
	User          *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"user,omitempty"`
}

// initialize handles setting up the API client
func (c *Cluster) initialize() error {
	// already initialized?
	if c.client != nil {
		log.Warningf("initialize called for cluster %s when it was already initialized, skipping", c.Hostname)
		return nil
	}
	if c.Username == "" {
		return fmt.Errorf("username must be set")
	}
	if c.Password == "" {
		return fmt.Errorf("password must be set")
	}
	if c.Hostname == "" {
		return fmt.Errorf("hostname must be set")
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
		Transport: tr,
		Jar:       jar,
	}
	c.baseURL = "https://" + c.Hostname + ":" + strconv.Itoa(c.Port)
	c.badStats = mapset.NewSet[string]()
	return nil
}

// String returns the string representation of Cluster as the cluster name
func (c *Cluster) String() string {
	return c.ClusterName
}

// Authenticate authenticates to the cluster using the session API endpoint
// and saves the cookies needed to authenticate subsequent requests
func (c *Cluster) Authenticate() error {
	var err error
	var resp *http.Response

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
	u, err := url.Parse(c.baseURL + sessionPath)
	if err != nil {
		return err
	}
	// POST our authentication request to the API
	// This may be our first connection so we'll retry here in the hope that if
	// we can't connect to one node, another may be responsive
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	retrySecs := 1
	for i := 1; i <= c.maxRetries; i++ {
		resp, err = c.client.Do(req)
		if err == nil {
			break
		}
		log.Warningf("Authentication request failed: %s - retrying in %d seconds", err, retrySecs)
		time.Sleep(time.Duration(retrySecs) * time.Second)
		retrySecs *= 2
		if retrySecs > maxTimeoutSecs {
			retrySecs = maxTimeoutSecs
		}
	}
	if err != nil {
		return fmt.Errorf("max retries exceeded for connect to %s, aborting connection attempt", c.Hostname)
	}
	defer resp.Body.Close()
	// 201(StatusCreated) is success
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Authenticate: auth failed - %s", resp.Status)
	}
	// parse out time limit so we can reauth when necessary
	dec := json.NewDecoder(resp.Body)
	var ar map[string]any
	err = dec.Decode(&ar)
	if err != nil {
		return fmt.Errorf("Authenticate: unable to parse auth response - %s", err)
	}
	// drain any other output
	io.Copy(io.Discard, resp.Body)
	var timeout int
	ta, ok := ar["timeout_absolute"]
	if ok {
		timeout = int(ta.(float64))
	} else {
		// This shouldn't happen, but just set it to a sane default
		log.Warning("authentication API did not return timeout value, using default")
		timeout = 14400
	}
	if timeout > 60 {
		timeout -= 60 // Give a minute's grace to the reauth timer
	}
	c.reauthTime = time.Now().Add(time.Duration(timeout) * time.Second)

	c.csrfToken = ""
	// Dig out CSRF token so we can set the appropriate header
	for _, cookie := range c.client.Jar.Cookies(u) {
		if cookie.Name == "isicsrf" {
			log.Debugf("Found csrf cookie %v\n", cookie)
			c.csrfToken = cookie.Value
		}
	}
	if c.csrfToken == "" {
		log.Debugf("No CSRF token found for cluster %s, assuming old-style session auth", c.Hostname)
	}

	return nil
}

// GetClusterConfig pulls information from the cluster config API
// endpoint, including the actual cluster name
func (c *Cluster) GetClusterConfig() error {
	var v any
	resp, err := c.restGet(configPath)
	if err != nil {
		return err
	}
	err = json.Unmarshal(resp, &v)
	if err != nil {
		return err
	}
	m := v.(map[string]any)
	version := m["onefs_version"]
	r := version.(map[string]any)
	release := r["version"]
	rel := release.(string)
	c.OSVersion = rel
	if c.PreserveCase {
		c.ClusterName = m["name"].(string)
	} else {
		c.ClusterName = strings.ToLower(m["name"].(string))
	}
	return nil
}

// Connect establishes the initial network connection to the cluster,
// then pulls the cluster config info to get the real cluster name
func (c *Cluster) Connect() error {
	var err error
	if err = c.initialize(); err != nil {
		return err
	}
	if c.AuthType == authtypeSession {
		if err = c.Authenticate(); err != nil {
			return err
		}
	}
	if err = c.GetClusterConfig(); err != nil {
		return err
	}
	return nil
}

// UnmarshalSummaryStatsProtocol unmarshals the JSON return from the summary stats protocol endpoint
func UnmarshalSummaryStatsProtocol(data []byte) (SummaryStatsProtocol, error) {
	var r SummaryStatsProtocol
	err := json.Unmarshal(data, &r)
	return r, err
}

// GetSummaryProtocolStats queries the summary stats protocol endpoint and returns a SummaryStatsProtocol struct or an error
func (c *Cluster) GetSummaryProtocolStats() ([]SummaryStatsProtocolItem, error) {
	path := summaryStatsPath + "protocol?degraded=true"
	log.Infof("fetching protocol summary stats from cluster %s", c)
	resp, err := c.restGet(path)
	if err != nil {
		log.Errorf("cluster %s failed to get protocol summary stats: %v\n", c, err)
		// TODO investigate handling partial errors rather than totally failing?
		return nil, err
	}
	// TODO - Need to handle JSON return of "errors" here (e.g. for re-auth
	// when using session cookies)
	log.Debugf("cluster %s got response %s", c, resp)
	r, err := UnmarshalSummaryStatsProtocol(resp)
	if err != nil {
		errmsg := fmt.Errorf("cluster %s unable to parse protocol summary stats response %q - error %s", c, resp, err)
		return nil, errmsg
	}
	if r.Errors != nil {
		// Theoretically, the Errors array can contain multiple entries
		// I haven't ever seen that, so we just take the first entry here
		apiError := r.Errors[0]
		errmsg := fmt.Errorf("protocol summary stats endpoint for cluster %s returned error code %s, message %s", c.ClusterName, apiError.Code, apiError.Message)
		return nil, errmsg
	}
	log.Debugf("cluster %s successfully decoded %d protocol summary stats", c, len(r.Protocol))
	return r.Protocol, nil
}

// UnmarshalSummaryStatsClient unmarshals the JSON return from the summary stats client endpoint
func UnMarshalSummaryStatsClient(data []byte) (SummaryStatsClient, error) {
	var r SummaryStatsClient
	err := json.Unmarshal(data, &r)
	return r, err
}

// GetSummaryClientStats queries the summary stats client endpoint and returns a SummaryStatsClient struct or an error
func (c *Cluster) GetSummaryClientStats() ([]SummaryStatsClientItem, error) {
	path := summaryStatsPath + "client?degraded=true"
	log.Infof("fetching client summary stats from cluster %s", c)
	resp, err := c.restGet(path)
	if err != nil {
		log.Errorf("cluster %s failed to get client summary stats: %v\n", c, err)
		// TODO investigate handling partial errors rather than totally failing?
		return nil, err
	}
	// TODO - Need to handle JSON return of "errors" here (e.g. for re-auth
	// when using session cookies)
	log.Debugf("cluster %s got response %s", c, resp)
	r, err := UnMarshalSummaryStatsClient(resp)
	if err != nil {
		errmsg := fmt.Errorf("cluster %s unable to parse client summary stats response %q - error %s", c, resp, err)
		return nil, errmsg
	}
	if r.Errors != nil {
		// Theoretically, the Errors array can contain multiple entries
		// I haven't ever seen that, so we just take the first entry here
		apiError := r.Errors[0]
		errmsg := fmt.Errorf("client summary stats endpoint for cluster %s returned error code %s, message %s", c.ClusterName, apiError.Code, apiError.Message)
		return nil, errmsg
	}
	log.Debugf("cluster %s successfully decoded %d client summary stats", c, len(r.Client))
	return r.Client, nil
}

// GetStats takes an array of statistics keys and returns an
// array of StatResult structures
func (c *Cluster) GetStats(stats []string) ([]StatResult, error) {
	var results []StatResult
	var buffer bytes.Buffer

	basePath := statsPath + "?degraded=true&devid=all&show_nodes=true"
	// length of key args
	la := 0
	// Need special case for short last get
	ls := len(stats)
	log.Infof("fetching %d stats from cluster %s", ls, c)
	// max minus (initial string + slop)
	maxlen := MaxAPIPathLen - (len(basePath) + 100)
	buffer.WriteString(basePath)
	for i, stat := range stats {
		// 5 == len("?key=")
		if la+5+len(stat) < maxlen {
			buffer.WriteString("&key=")
			buffer.WriteString(stat)
			if i != ls-1 {
				continue
			}
		}
		log.Debugf("cluster %s fetching %s", c, buffer.String())
		resp, err := c.restGet(buffer.String())
		if err != nil {
			log.Errorf("cluster %s failed to get stats: %v\n", c, err)
			// TODO investigate handling partial errors rather than totally failing?
			return nil, err
		}
		// TODO - Need to handle JSON return of "errors" here (e.g. for re-auth
		// when using session cookies)
		log.Debugf("cluster %s got response %s", c, resp)
		r, err := parseStatResult(resp)
		if err != nil {
			log.Errorf("cluster %s unable to parse response %q - error %s\n", c, resp, err)
			return nil, err
		}
		log.Debugf("cluster %s parsed stats results = %v", c, r)
		results = append(results, r...)
		buffer.Reset()
	}
	return results, nil
}

// parseStatResult is currently very basic and just unmarshals the JSON API return
func parseStatResult(res []byte) ([]StatResult, error) {
	sa := struct {
		Stats []StatResult `json:"stats"`
	}{}
	err := json.Unmarshal(res, &sa)
	if err == nil {
		return sa.Stats, nil
	}
	var errors []ApiError
	err = json.Unmarshal(res, &errors)
	if err != nil {
		errmsg := fmt.Errorf("unable to parse current stats endpoint result: %s", res)
		return nil, errmsg
	}
	// Theoretically, the Errors array can contain multiple entries
	// I haven't ever seen that, so we just take the first entry here
	apiError := errors[0]
	errmsg := fmt.Errorf("stats endpoint returned error code %s, message %s", apiError.Code, apiError.Message)
	return nil, errmsg
}

// fetchStatDetails gathers and returns the API-provided metadata for the given set of stats
func (c *Cluster) fetchStatDetails(sg map[string]statGroup) map[string]statDetail {
	badStat := statDetail{valid: false}

	statInfo := make(map[string]statDetail)
	for group := range sg {
		stats := sg[group].stats
		for _, stat := range stats {
			path := statInfoPath + stat
			resp, err := c.restGet(path)
			if err != nil {
				log.Warningf("cluster %s failed to retrieve information for stat %s - %s - removing", c, stat, err)
				statInfo[stat] = badStat
				continue
			}
			// parse stat info
			detail, err := parseStatInfo(resp)
			if err != nil {
				log.Warningf("cluster %s failed to parse detailed information for stat %s - %s - removing", c, stat, err)
				statInfo[stat] = badStat
				continue
			}
			statInfo[stat] = *detail
		}
	}
	return statInfo
}

// parseStatInfo parses the OneFS API statistics metric metadata returned
// from the statistics detail endpoint
func parseStatInfo(res []byte) (*statDetail, error) {
	var detail statDetail
	var v any

	// Unmarshal the JSON return first
	err := json.Unmarshal(res, &v)
	if err != nil {
		return nil, err
	}

	m := v.(map[string]any)
	// Did the API throw an error?
	if ea, ok := m["errors"]; ok {
		// handle API error return here
		// I've never seen more than one error in the array, but we handle it anyway
		ea := ea.([]any)
		es := bytes.NewBufferString("Error: ")
		for _, e := range ea {
			e := e.(map[string]any)
			es.WriteString(fmt.Sprintf("code: %q, message: %q", e["code"], e["message"]))
		}
		return nil, fmt.Errorf("%s", es.String())
	}

	var keys any
	var ok bool
	if keys, ok = m["keys"]; !ok {
		// If we didn't get an error above, we should have got a valid return
		return nil, fmt.Errorf("unexpected JSON return %#v", m)
	}
	ka := keys.([]any)
	for _, k := range ka {
		// pull info from key
		k := k.(map[string]any)
		// Extract stat update times out of "policies" if they exist
		kp := k["policies"]
		if kp == nil {
			// 0 == no defined update interval i.e. on-demand
			detail.updateIntvl = 0.0
		} else {
			kpa := kp.([]any)
			for _, pol := range kpa {
				pol := pol.(map[string]any)
				// we only want the current info, not the historical
				if pol["persistent"] == false {
					detail.updateIntvl = pol["interval"].(float64)
					break
				}
			}
		}
		detail.description = k["description"].(string)
		detail.units = k["units"].(string)
		detail.scope = k["scope"].(string)
		detail.datatype = k["type"].(string)
		detail.aggType = k["aggregation_type"].(string)
		// key := k["key"]
	}

	detail.valid = true
	return &detail, nil
}

// isConnectionRefused checks if the given error is a connection refused error
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

// restGet returns the REST response for the given endpoint from the API
func (c *Cluster) restGet(endpoint string) ([]byte, error) {
	var err error
	var resp *http.Response

	if c.AuthType == authtypeSession && time.Now().After(c.reauthTime) {
		log.Infof("re-authenticating to cluster %s based on timer", c)
		if err = c.Authenticate(); err != nil {
			return nil, err
		}
	}

	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, err
	}
	req, err := c.newGetRequest(u.String())
	if err != nil {
		return nil, err
	}

	retrySecs := 1
	for i := 1; i < c.maxRetries; i++ {
		resp, err = c.client.Do(req)
		if err == nil {
			// We got a valid http response
			if resp.StatusCode == http.StatusOK {
				break
			}
			resp.Body.Close()
			// check for need to re-authenticate (maybe we are talking to a different node)
			if resp.StatusCode == http.StatusUnauthorized {
				if c.AuthType == authtypeBasic {
					return nil, fmt.Errorf("basic authentication for cluster %s failed - check username and password", c)
				}
				log.Noticef("Session-based authentication to cluster %s failed, attempting to re-authenticate", c)
				if err = c.Authenticate(); err != nil {
					return nil, err
				}
				req, err = c.newGetRequest(u.String())
				if err != nil {
					return nil, err
				}
				continue
				// TODO handle repeated auth failures to avoid panic
			}
			return nil, fmt.Errorf("Cluster %s returned unexpected HTTP response: %v", c, resp.Status)
		}
		// assert err != nil
		// TODO - consider adding more retryable cases e.g. temporary DNS hiccup
		if !isConnectionRefused(err) {
			return nil, err
		}
		log.Errorf("Connection to cluster %s (host %s) refused, retrying in %d seconds", c.ClusterName, c.Hostname, retrySecs)
		time.Sleep(time.Duration(retrySecs) * time.Second)
		retrySecs *= 2
		if retrySecs > maxTimeoutSecs {
			retrySecs = maxTimeoutSecs
		}
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Cluster %s returned unexpected HTTP response: %v", c, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	return body, err
}

// newGetRequest creates a new HTTP GET request with the appropriate headers
// and authentication information
func (c *Cluster) newGetRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	if c.AuthType == authtypeBasic {
		req.SetBasicAuth(c.AuthInfo.Username, c.AuthInfo.Password)
	}
	if c.csrfToken != "" {
		// Must be newer session-based auth with CSRF protection
		req.Header.Set("X-CSRF-Token", c.csrfToken)
		req.Header.Set("Referer", c.baseURL)
	}
	return req, nil
}
