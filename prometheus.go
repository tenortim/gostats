package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusSink defines the data to allow us talk to an Prometheus database
type PrometheusSink struct {
	cluster   string
	reg       prometheus.Registerer
	port      uint64
	metricMap map[string]*PrometheusStat
}

const NAMESPACE = "isilon"

// promStatInternal holds the Prometheus metric name and the implementation which is always GaugeVec
type promStatInternal struct {
	name  string
	gauge *prometheus.GaugeVec
}

// PrometheusStat holds the necessary stat metadata for the Prometheus backend
// this includes API stats metadata, whether the stats is multivalued and a mapping
// of the stat fields to the internal detail (gauge pointer)
type PrometheusStat struct {
	detail  statDetail
	isMulti bool
	fields  map[string]promStatInternal
}

// GetPrometheusWriter returns an Prometheus DBWriter
func GetPrometheusWriter() DBWriter {
	return &PrometheusSink{}
}

// promStatBasename returns a Prometheus-style snakecase base name for the given stat name
func promStatBasename(stat string) string {
	return strings.ReplaceAll(stat, ".", "_")
	// XXX handle problematic naming here too
}

// promStatNameWithField returns a Prometheus-style snakecase stat name for the given
// base name and metric field
func promStatNameWithField(basename string, field string) string {
	return basename + "_" + field
	// XXX handle problematic naming here too
}

// BasicAuth wraps a handler requiring HTTP basic auth for it using the given
// username and password and the specified realm, which shouldn't contain quotes.
//
// Most web browser display a dialog with something like:
//
//	The website says: "<realm>"
//
// Which is really stupid so you may want to set the realm to a message rather than
// an actual realm.
func BasicAuth(handler http.HandlerFunc, username, password, realm string) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		user, pass, ok := r.BasicAuth()

		if !ok || user != username || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
			w.WriteHeader(401)
			w.Write([]byte("Unauthorised.\n"))
			return
		}

		handler(w, r)
	}
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

// findExternalAddr attempt to find a reachable external IP address for the system
func findExternalAddr() (string, error) {
	// Discover local (listener) IP address
	// Prefer IPv4 addresses
	// If multiple are found default to the first
	var listenAddr string

	ips, err := ListExternalIPs()
	if err != nil {
		return "", fmt.Errorf("unable to list external IP addresses: %v", err)
	}
	for _, ip := range ips {
		if IsIPv4(ip.String()) {
			listenAddr = ip.String()
		}
	}
	if listenAddr == "" {
		// No IPv4 addresses found, choose the first IPv6 address
		if len(ips) == 0 {
			return "", fmt.Errorf("no valid external IP addresses found")
		}
		listenAddr = ips[0].String()
	}
	return listenAddr, nil
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

// Init initializes an PrometheusSink so that points can be written
// The array of argument strings comprises host, port, database
func (s *PrometheusSink) Init(clusterName string, cluster clusterConf, args []string, sd map[string]statDetail) error {
	var username, password string
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
	s.port = *port

	if authenticated {
		username = args[0]
		password = args[1]
	}

	reg := prometheus.NewRegistry()
	s.reg = reg

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
		fields := make(map[string]promStatInternal)
		switch detail.datatype {
		case "int32", "int64", "double", "uint64":
			name := basename
			gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: NAMESPACE,
				Name:      name,
				Help:      detail.description,
			}, labels)
			reg.MustRegister(gauge)
			fields["value"] = promStatInternal{name: name, gauge: gauge}
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
				gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
					Namespace: NAMESPACE,
					Name:      name,
					Help:      detail.description,
				}, slabels)
				reg.MustRegister(gauge)
				fields[field] = promStatInternal{name: name, gauge: gauge}
				promstat := PrometheusStat{detail: detail, isMulti: true, fields: fields}
				metricMap[stat] = &promstat
			}
		case "stats_cache_data_v2":
			for _, field := range statCacheFields {
				name := promStatNameWithField(basename, field)
				gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
					Namespace: NAMESPACE,
					Name:      name,
					Help:      detail.description,
				}, labels)
				reg.MustRegister(gauge)
				fields[field] = promStatInternal{name: name, gauge: gauge}
				promstat := PrometheusStat{detail: detail, isMulti: true, fields: fields}
				metricMap[stat] = &promstat
			}
		default:
			log.Errorf("Unknown metric type %v for stat %s detail %+v, skipping", detail.datatype, stat, detail)
		}
	}
	s.metricMap = metricMap

	// Set up http server here
	mux := http.NewServeMux()
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	if authenticated {
		handlefunc := BasicAuth(handler.ServeHTTP, username, password, "auth required to access metrics")
		mux.HandleFunc("/metrics", handlefunc)
	} else {
		mux.Handle("/metrics", handler)
	}
	addr := fmt.Sprintf(":%d", s.port)
	// XXX improve error handling here?
	go func() { log.Error(http.ListenAndServe(addr, mux)) }()

	return nil
}

// WriteStats takes an array of StatResults and writes them to Prometheus
func (s *PrometheusSink) WriteStats(stats []StatResult) error {
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

		if !promstat.isMulti {
			_, ok := promstat.fields["value"]
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
			promstat.fields["value"].gauge.With(labels).Set(value)
			continue
		}
		// multivalued stat e.g. proto stats detail
		for i, fields := range fa {
			for k, v := range fields {
				labels := make(prometheus.Labels)
				labels["cluster"] = s.cluster
				if stat.Devid != 0 {
					labels["node"] = strconv.Itoa(stat.Devid)
				}
				for tag, value := range ta[i] {
					log.Debugf("setting label %v to %v", tag, value)
					labels[tag] = value
				}
				// ugly special case handling
				// we skipped "op_id" since there's no point creating a separate metric, but the API will still return it
				// so for now, hardcode it to be skipped
				if k == "op_id" {
					continue
				}
				psi, ok := promstat.fields[k]
				if !ok {
					log.Errorf("attempt to access invalid field at key %v", k)
					panic("attempt to access invalid field")
				}
				log.Debugf("setting metric %v to %v", psi.name, v.(float64))
				psi.gauge.With(labels).Set(v.(float64))
			}
		}
	}

	return nil
}
