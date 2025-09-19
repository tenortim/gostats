# Gostats

Gostats is a tool that can be used to query multiple OneFS clusters for statistics data via Isilon's OneFS API (PAPI). It uses a pluggable backend module for processing the results of those queries.
The current version supports three backend types: [Influxdb](https://www.influxdata.com/), [Prometheus](https://prometheus.io/), and a no-op discard backend useful for testing.
The InfluxDB backend sends query results to an InfluxDB server. The Prometheus backend spawns an http Web server per-cluster that serves the metrics via the "/metrics" endpoint.
The Grafana dashboards provided with the data insights project may be used without modification with the Go version of the collector.

## Installation Instructions

The current version of gostats requires Golang version 1.21 or higher to build.

* $ go build

## Run Instructions

* Rename or copy the example configuration file, example_isi_data_insights_d.toml to idic.toml. The path ./idic.toml is the default configuration file path for the Go version of the connector. If you use that name and run the connector from the source directory then you don't have to use the -config-file parameter to specify a different configuration file.
* Edit the idic.toml file so that it is set up to query the set of Dell PowerScale OneFS clusters that you wish to monitor. Do this by modifying and replicating the cluster config section.
* The example configuration file is configured to send several sets of stats to InfluxDB via the influxdb.go backend. If you intend to use the default backend, you will need to install InfluxDB. InfluxDB can be installed locally (i.e on the same system as the connector) or remotely (i.e. on a different system). Follow the [install instructions](https://portal.influxdata.com/downloads/) but install "indluxdb" not "influxdb2"

* If you installed InfluxDB to somewhere other than localhost and/or port 8086, then you'll also need to update the configuration file with the address and port of the InfluxDB service.
* If using InfluxDB, you must create the "isi_data_insights" database before running the collectors:

    ```sh
     influx -host localhost -port 8086 -execute 'create database isi_data_insights'
     ```

* Create a local user on each cluster and grant the required privileges:

    ```sh
    isi auth users create --email=stat.user@mydomain.com --enabled=true --name=statsreader --password='s3kret_pass'
    isi auth roles create --name='StatsReader' --description='Role to allow reading of statistics via PAPI'
    isi auth roles modify StatsReader --add-priv=ISI_PRIV_STATISTICS --add-priv-ro=ISI_PRIV_LOGIN_PAPI --add-user=statsreader
    ```

* To run the connector in the background:

    ```sh
    (nohup ./gostats &)
    ```

* If you wish to use Prometheus as the backend target, configure it in the "global" section of the config file and add a "prometheus_port" to each configured cluster stanze. This will spawn a Prometheus http metrics listener on the configured port.
  
## Customizing the connector

The connector is designed to allow for customization via a plugin architecture. The original plugin, influxdb.go, can be configured via the provided example configuration file. If you would like to process the stats data differently or send them to a different backend than the influxdb.go you can use one of the other provided backend processors or you can implement your own custom stats processor. The backend interface type is defined in statssink.go. Here are the instructions for creating a new backend:

* Create a file called my_plugin.go, or whatever you want to name it.
* In the my_plugin.go file define the following:
  * a structure that retains the information needed for the stats-writing function to be able to send data to the backend. Influxdb example:

    ```go
    type InfluxDBSink struct {
      cluster  string
      client   client.Client
      bpConfig client.BatchPointsConfig
      badStats mapset.Set[string]
    }
    ```

  * a function with signature

    ```go
    func (s *InfluxDBSink) Init(clustername string, config *tomlConfig, ci int, sd map[string]statDetail) error
    ```

    that takes as input the name/ip-address of a cluster, the global config, the index into the config.Clusters struct for this cluster, and a map of all of the configured stats, and which initializes the receiver.
  * Also define a point-writing function with the following signature:

    ```go
    func (s *InfluxDBSink) WritePoints(points []Point) error
    ```

    Point is defined in backend.go

* Add the my_plugin.go file to the source directory.
* Add code to getDBWriter() in main.go to recognize your new backend.
* Update the config file with the name of your plugin (i.e. 'my_plugin')
* Rebuild and restart the gostats tool.
