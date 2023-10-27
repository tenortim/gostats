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

// promMetric holds the Prometheus metadata exposed by the "/metrics"
// endpoint for a given partitioned performance stat within a dataset
type promMetric struct {
	name        string
	description string
	labels      []string
}

// PrometheusStat holds the necessary stat metadata for the Prometheus backend
// this includes API stats metadata, whether the stats is multivalued and a mapping
// of the stat fields to the internal detail (gauge pointer)
type PrometheusStat struct {
	detail  statDetail
	isMulti bool
	fields  map[string]promMetric
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
// The array of argument strings comprises host, port, database
func (s *PrometheusSink) Init(clusterName string, cluster clusterConf, args []string, sd map[string]statDetail) error {
	authenticated := false
	// args are either nothing, or, optionally, a username and password to support basic auth on the metrics endpoint
	switch len(args) {
	case 0:
		authenticated = false
	case 2:
		authenticated = true
	default:
		return fmt.Errorf("prometheus Init() wrong number of args %d - expected 0 or 2", len(args))
	}

	s.cluster = clusterName
	port := cluster.PrometheusPort
	if port == nil {
		return fmt.Errorf("prometheus plugin initialization failed - missing port definition for cluster %v", clusterName)
	}
	s.client.ListenPort = *port

	if authenticated {
		s.client.BasicUsername = args[0]
		s.client.BasicPassword = args[1]
	}

	registry := prometheus.NewRegistry()
	s.client.registry = registry
	registry.Register(s)

	s.fam = make(map[string]*MetricFamily)

	// protoStatsFields details the metric values that the protostats endpoint returns for each protocol
	protoStatsFields := []string{
		// tagged by op_name so no point in creating another (useless) metric
		// "op_id",
		"op_count", "op_rate", "in_min", "in_max", "in_rate", "in_std_dev", "out_min", "out_max", "out_rate", "out_std_dev", "time_min", "time_max", "time_avg", "time_std_dev",
	}

	// statCacheFields details the metric values that the OneFS cache statistics endpoint returns
	statCacheFields := []string{
		// L1 stats
		//  data stats
		//   read stats
		"l1_data_read_start", "l1_data_read_hit", "l1_data_read_miss", "l1_data_read_wait",
		//   async read stats
		"l1_data_aread_start", "l1_data_aread_hit", "l1_data_aread_miss", "l1_data_aread_wait",
		//   prefetch stats
		"l1_data_prefetch_start", "l1_data_prefetch_hit",
		//  metadata stats
		//   read stats
		"l1_meta_read_start", "l1_meta_read_hit", "l1_meta_read_miss", "l1_meta_read_wait",
		//   prefetch stats
		"l1_meta_prefetch_start", "l1_meta_prefetch_hit",

		// L2 stats
		//  data stats
		//   read stats
		"l2_data_read_start", "l2_data_read_hit", "l2_data_read_miss", "l2_data_read_wait",
		//   prefetch stats
		"l2_data_prefetch_start", "l2_data_prefetch_hit",
		//  metadata stats
		//   read stats
		"l2_meta_read_start", "l2_meta_read_hit", "l2_meta_read_miss", "l2_meta_read_wait",
		//   prefetch stats
		"l2_meta_prefetch_start", "l2_meta_prefetch_hit",

		// top level stats
		"l1_prefetch_miss", "l2_prefetch_miss", "oldest_page_age",

		// L3 stats
		//  data stats
		//   read stats
		"l3_data_read_start", "l3_data_read_hit", "l3_data_read_miss", "l3_data_read_wait",
		//   prefetch stats
		"l3_data_prefetch_start", "l3_data_prefetch_hit",
		//  metadata stats
		//   read stats
		"l3_meta_read_start", "l3_meta_read_hit", "l3_meta_read_miss", "l3_meta_read_wait",
		//   prefetch stats
		"l3_meta_prefetch_start", "l3_meta_prefetch_hit",
	}

	metricMap := make(map[string]*PrometheusStat)

	clusterLabels := []string{"cluster"}
	nodeLabels := []string{"cluster", "node"}
	for stat, detail := range sd {
		labels := clusterLabels
		if detail.scope == "node" {
			labels = nodeLabels
		}
		basename := promStatBasename(stat)
		fields := make(map[string]promMetric)
		switch detail.datatype {
		case "int32", "int64", "double", "uint64":
			name := basename
			// gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			// 	Namespace: NAMESPACE,
			// 	Name:      name,
			// 	Help:      detail.description,
			// }, labels)
			// reg.MustRegister(gauge)
			fields["value"] = promMetric{name: name, description: detail.description, labels: labels}
			promstat := PrometheusStat{detail: detail, isMulti: false, fields: fields}
			metricMap[stat] = &promstat
		case "stats_proto_opstat_list":
			slabels := make([]string, len(labels))
			copy(slabels, labels)
			// break out stats have class and op name fields
			// total stats do not
			if !strings.HasSuffix(stat, ".total") {
				slabels = append(labels, "class_name", "op_name")
			}
			for _, field := range protoStatsFields {
				name := promStatNameWithField(basename, field)
				fields[field] = promMetric{name: name, description: detail.description, labels: slabels}
			}
			promstat := PrometheusStat{detail: detail, isMulti: true, fields: fields}
			metricMap[stat] = &promstat
		case "stats_cache_data_v2":
			for _, field := range statCacheFields {
				name := promStatNameWithField(basename, field)
				fields[field] = promMetric{name: name, description: detail.description, labels: labels}
			}
			promstat := PrometheusStat{detail: detail, isMulti: true, fields: fields}
			metricMap[stat] = &promstat
		default:
			log.Errorf("Unknown metric type %v for stat %s detail %+v, skipping", detail.datatype, stat, detail)
		}
	}
	s.metricMap = metricMap

	// Set up http server here
	err := s.client.Connect()

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

		promstat := s.metricMap[stat.Key]
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
		if !promstat.isMulti {
			sampleID := CreateSampleID(ta[0])
			metric, ok := promstat.fields["value"]
			if !ok {
				log.Errorf("Unexpected missing value for stat %v", stat.Key)
				panic("unexpected null pointer")
			}
			value, ok := stat.Value.(float64)
			if !ok {
				log.Errorf("Unexpected null value for stat %v", stat.Key)
				log.Errorf("stats = %+v, fa = %+v", stat, fa)
				panic("unexpected null value")
			}
			labels := make(prometheus.Labels)
			labels["cluster"] = s.cluster
			if stat.Devid != 0 {
				labels["node"] = strconv.Itoa(stat.Devid)
			}
			// promstat.fields["value"].gauge.With(labels).Set(value)
			sample := &Sample{
				Labels:     labels,
				Value:      value,
				Timestamp:  time.Unix(stat.UnixTime, 0),
				Expiration: now.Add(expiration),
			}
			s.addMetricFamily(sample, metric.name, metric.description, sampleID)
			continue
		}
		// multivalued stat e.g. proto stats detail
		for i, fields := range fa {
			for k, v := range fields {
				// ugly special case handling
				// we drop "op_id" since there's no point creating a separate metric, but the API will still return it
				// so for now, hardcode it to be skipped
				if k == "op_id" {
					continue
				}
				sampleID := CreateSampleID(ta[i])
				metric, ok := promstat.fields[k]
				if !ok {
					log.Errorf("attempt to access invalid field at key %v", k)
					panic("attempt to access invalid field")
				}
				labels := make(prometheus.Labels)
				labels["cluster"] = s.cluster
				if stat.Devid != 0 {
					labels["node"] = strconv.Itoa(stat.Devid)
				}
				for tag, value := range ta[i] {
					log.Debugf("setting label %v to %v", tag, value)
					labels[tag] = value
				}

				log.Debugf("setting metric %v to %v", metric.name, v.(float64))
				// psi.gauge.With(labels).Set(v.(float64))
				sample := &Sample{
					Labels:     labels,
					Value:      v.(float64),
					Timestamp:  time.Unix(stat.UnixTime, 0),
					Expiration: now.Add(expiration),
				}
				s.addMetricFamily(sample, metric.name, metric.description, sampleID)
			}
		}
	}

	return nil
}
