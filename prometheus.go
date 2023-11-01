package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusClient holds the metadata for the required networking (http) functionality
type PrometheusClient struct {
	ListenPort    uint64
	TLSCert       string `toml:"tls_cert"`
	TLSKey        string `toml:"tls_key"`
	BasicUsername string `toml:"basic_username"`
	BasicPassword string `toml:"basic_password"`

	server   *http.Server
	registry *prometheus.Registry
}

// PrometheusSink defines the data to allow us talk to an Prometheus database
type PrometheusSink struct {
	cluster   string
	client    PrometheusClient
	metricMap map[string]*PrometheusStat

	sync.Mutex
	fam map[string]*MetricFamily
}

const NAMESPACE = "isilon"
const BASESTATNAME = "stat"

// PrometheusStat holds the necessary stat metadata for the Prometheus backend
// this includes API stats metadata, whether the stats is multivalued and a mapping
// of the stat fields to the internal detail (gauge pointer)
type PrometheusStat struct {
	detail      statDetail
	description string
}

// SampleID uniquely identifies a Sample
type SampleID string

// Sample represents the current value of a series.
type Sample struct {
	// Labels are the Prometheus labels.
	Labels map[string]string
	Value  float64
	// Metric timestamp
	Timestamp time.Time
	// Expiration is the deadline that this Sample is valid until.
	Expiration time.Time
}

// MetricFamily contains the data required to build valid prometheus Metrics.
type MetricFamily struct {
	// Samples are the Sample belonging to this MetricFamily.
	Samples map[SampleID]*Sample
	// LabelSet is the label counts for all Samples.
	LabelSet map[string]int
	// Desc contains the detailed description for this metric
	Desc string
}

// GetPrometheusWriter returns an Prometheus DBWriter
func GetPrometheusWriter() DBWriter {
	return &PrometheusSink{}
}

// promStatBasename returns a Prometheus-style snakecase base name for the given stat name
func promStatBasename(stat string) string {
	return NAMESPACE + "_" + BASESTATNAME + "_" + strings.ReplaceAll(stat, ".", "_")
	// XXX handle problematic naming here too
}

// promStatNameWithField returns a Prometheus-style snakecase stat name for the given
// base name and metric field
func promStatNameWithField(basename string, field string) string {
	return basename + "_" + field
	// XXX handle problematic naming here too
}

func (p *PrometheusClient) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.BasicUsername != "" && p.BasicPassword != "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)

			username, password, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(username), []byte(p.BasicUsername)) != 1 ||
				subtle.ConstantTimeCompare([]byte(password), []byte(p.BasicPassword)) != 1 {
				http.Error(w, "Not authorized", http.StatusUnauthorized)
				return
			}
		}

		h.ServeHTTP(w, r)
	})
}

type httpSdConf struct {
	ListenIP    string
	ListenPorts []uint64
}

func (h *httpSdConf) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var listenAddrs string
	w.Header().Set("Content-Type", "application/json")
	sdstr1 := `[
	{
		"targets": [`
	for i, port := range h.ListenPorts {
		if i != 0 {
			listenAddrs += ", "
		}
		listenAddrs += fmt.Sprintf("\"%s:%d\"", h.ListenIP, port)
	}
	sdstr2 := `],
		"labels": {
			"__meta_prometheus_job": "isilon_stats"
		}
	}
]`
	w.Write([]byte(sdstr1 + listenAddrs + sdstr2))
}

// Start an http listener in a goroutine to server Prometheus HTTP SD requests
func startPromSdListener(conf tomlConfig) error {
	var listenAddr string
	var err error
	listenAddr = conf.PromSD.ListenAddr
	if listenAddr == "" {
		listenAddr, err = findExternalAddr()
		if err != nil {
			return err
		}
	}
	var promPorts []uint64
	for _, cl := range conf.Clusters {
		if cl.PrometheusPort != nil {
			promPorts = append(promPorts, *cl.PrometheusPort)
		}
	}
	h := httpSdConf{ListenIP: listenAddr, ListenPorts: promPorts}
	// Create listener
	mux := http.NewServeMux()
	mux.Handle("/", &h)
	addr := fmt.Sprintf(":%d", conf.PromSD.SDport)
	// XXX improve error handling here?
	go func() { log.Error(http.ListenAndServe(addr, mux)) }()
	return nil
}

func (p *PrometheusClient) Connect() error {
	addr := fmt.Sprintf(":%d", p.ListenPort)

	mux := http.NewServeMux()
	mux.Handle("/metrics", p.auth(promhttp.HandlerFor(
		p.registry, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError})))

	p.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		var err error
		if p.TLSCert != "" && p.TLSKey != "" {
			err = p.server.ListenAndServeTLS(p.TLSCert, p.TLSKey)
		} else {
			err = p.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Errorf("error creating prometheus metric endpoint, err: %s\n",
				err.Error())
		}
	}()

	return nil
}

// Init initializes an PrometheusSink so that points can be written
func (s *PrometheusSink) Init(clusterName string, config *tomlConfig, ci int, sd map[string]statDetail) error {
	s.cluster = clusterName
	promconf := config.Prometheus
	port := config.Clusters[ci].PrometheusPort
	if port == nil {
		return fmt.Errorf("prometheus plugin initialization failed - missing port definition for cluster %v", clusterName)
	}
	pc := s.client
	pc.ListenPort = *port

	if promconf.Authenticated {
		pc.BasicUsername = promconf.Username
		pc.BasicPassword = promconf.Password
	}
	pc.TLSCert = config.Prometheus.TLSCert
	pc.TLSKey = config.Prometheus.TLSKey

	registry := prometheus.NewRegistry()
	pc.registry = registry
	registry.Register(s)

	s.fam = make(map[string]*MetricFamily)

	metricMap := make(map[string]*PrometheusStat)

	for stat, detail := range sd {
		promstat := PrometheusStat{detail: detail, description: detail.description}
		metricMap[stat] = &promstat
	}
	s.metricMap = metricMap

	// Set up http server here
	err := pc.Connect()

	return err
}

func (s *PrometheusSink) Description() string {
	return "Configuration for the Prometheus client to spawn"
}

// Implements prometheus.Collector
func (s *PrometheusSink) Describe(ch chan<- *prometheus.Desc) {
	prometheus.NewGauge(prometheus.GaugeOpts{Name: "Dummy", Help: "Dummy"}).Describe(ch)
}

// Expire removes Samples that have expired.
func (s *PrometheusSink) Expire() {
	now := time.Now()
	for name, family := range s.fam {
		for key, sample := range family.Samples {
			// if s.ExpirationInterval.Duration != 0 && now.After(sample.Expiration) {
			if now.After(sample.Expiration) {
				for k := range sample.Labels {
					family.LabelSet[k]--
				}
				delete(family.Samples, key)

				if len(family.Samples) == 0 {
					delete(s.fam, name)
				}
			}
		}
	}
}

// Collect implements prometheus.Collector
func (s *PrometheusSink) Collect(ch chan<- prometheus.Metric) {
	s.Lock()
	defer s.Unlock()

	s.Expire()

	for name, family := range s.fam {
		// Get list of all labels on MetricFamily
		var labelNames []string
		for k, v := range family.LabelSet {
			if v > 0 {
				labelNames = append(labelNames, k)
			}
		}

		for _, sample := range family.Samples {
			desc := prometheus.NewDesc(name, family.Desc, labelNames, nil)
			// Get labels for this sample; unset labels will be set to the
			// empty string
			var labels []string
			for _, label := range labelNames {
				v := sample.Labels[label]
				labels = append(labels, v)
			}

			var metric prometheus.Metric
			var err error
			metric, err = prometheus.NewConstMetric(desc, prometheus.GaugeValue, sample.Value, labels...)
			if err != nil {
				log.Errorf("error creating prometheus metric, "+
					"key: %s, labels: %v,\nerr: %s\n",
					name, labels, err.Error())
			}

			metric = prometheus.NewMetricWithTimestamp(sample.Timestamp, metric)
			ch <- metric
		}
	}
}

// XXX We will use this when we convert the InfluxDB collector to use the full names
// those names will be separated by periods, and this will convert them.
// func sanitize(value string) string {
// 	return invalidNameCharRE.ReplaceAllString(value, "_")
// }

// CreateSampleID creates a SampleID based on the tags of a OneFS.Metric.
func CreateSampleID(tags map[string]string) SampleID {
	pairs := make([]string, 0, len(tags))
	for k, v := range tags {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(pairs)
	return SampleID(strings.Join(pairs, ","))
}

func addSample(fam *MetricFamily, sample *Sample, sampleID SampleID) {

	for k := range sample.Labels {
		fam.LabelSet[k]++
	}

	fam.Samples[sampleID] = sample
}

func (s *PrometheusSink) addMetricFamily(sample *Sample, mname string, desc string, sampleID SampleID) {
	var fam *MetricFamily
	var ok bool
	if fam, ok = s.fam[mname]; !ok {
		fam = &MetricFamily{
			Samples:  make(map[SampleID]*Sample),
			LabelSet: make(map[string]int),
			Desc:     desc,
		}
		s.fam[mname] = fam
	}

	addSample(fam, sample, sampleID)
}

// WriteStats takes an array of StatResults and exposes them on the /metrics endpoint
func (s *PrometheusSink) WriteStats(stats []StatResult) error {
	// Currently only one thread writing at any one time, but let's protect ourselves
	s.Lock()
	defer s.Unlock()

	now := time.Now()

	for _, stat := range stats {
		var fa []ptFields
		var ta []ptTags
		var err error

		promstat, ok := s.metricMap[stat.Key]
		if !ok {
			log.Fatalf("unable to find metric map entry for stat %+v", stat)
		}
		if !promstat.detail.valid {
			log.Debugf("skipping invalid stat %v", stat.Key)
			continue
		}
		if stat.ErrorCode != 0 {
			log.Warningf("Unable to retrieve stat %v, error %v, code %v", stat.Key, stat.ErrorString, stat.ErrorCode)
			if stat.ErrorCode == 9 {
				// Some stats are not valid on some configurations e.g. virtual, so drop them.
				log.Warningf("setting stat %v to invalid", stat.Key)
				s.metricMap[stat.Key].detail.valid = false
			}
			continue
		}
		fa, ta, err = DecodeStat(s.cluster, stat)
		if err != nil {
			// TODO consider trying to recover/handle errors
			log.Panicf("Failed to decode stat %+v: %s\n", stat, err)
		}
		if len(fa) == 0 {
			continue
		}

		// expire the stats based off their update interval
		expiration := time.Duration(promstat.detail.updateIntvl) * time.Second
		// Clamp value: cf calcBuckets() in main.go
		if expiration < 5 {
			expiration = time.Duration(5 * time.Second)
		}
		for i, fields := range fa {
			sampleID := CreateSampleID(ta[i])
			labels := make(prometheus.Labels)
			labels["cluster"] = s.cluster
			if stat.Devid != 0 {
				labels["node"] = strconv.Itoa(stat.Devid)
			}
			// is this a multi-valued stat e.g., proto stats detail?
			multiValued := false
			if len(fields) > 1 {
				multiValued = true
			}
			basename := promStatBasename(stat.Key)
			for k, v := range fields {
				var name string
				// ugly special case handling
				// we drop "op_id" since there's no point creating a separate metric, but the API will still return it
				// so for now, hardcode it to be skipped
				if k == "op_id" {
					continue
				}
				if !multiValued {
					name = basename
				} else {
					name = promStatNameWithField(basename, k)
				}
				value, ok := v.(float64)
				if !ok {
					log.Errorf("cannot convert field value for stat %v to float64", stat.Key)
					log.Errorf("stat = %+v, field = %+v", stat, k)
					panic("unexpected unconvertable value")
				}
				for tag, value := range ta[i] {
					log.Debugf("setting label %v to %v", tag, value)
					labels[tag] = value
				}

				log.Debugf("setting metric %v to %v", name, v.(float64))
				sample := &Sample{
					Labels:     labels,
					Value:      value,
					Timestamp:  time.Unix(stat.UnixTime, 0),
					Expiration: now.Add(expiration),
				}
				s.addMetricFamily(sample, name, promstat.description, sampleID)
			}
		}

	}
	return nil
}
