# Gostats

Gostats is a tool that can be used to query multiple OneFS clusters for statistics data via Isilon's OneFS API (PAPI). It uses a pluggable backend module for processing the results of those queries. The provided stat processor, defined in influxdb.go, sends query results to an InfluxDB backend. The backend interface type is defined in statssink.go. The Grafana dashboards provided with the data insights project may be used without modification with the Go version of the collector.

## Installation Instructions

* $ go build

## Run Instructions

* Rename or copy the example configuration file, example_isi_data_insights_d.toml to idic.toml. The path ./idic.toml is the default configuration file path for the Go version of the connector. If you use that name and run the connector from the source directory then you don't have to use the -config-file parameter to specify a different configuration file.
* Next edit the idic.toml file so that it is set up to query the set of Isilon OneFS clusters that you want to monitor. Do this by modifying and replicating the cluster config section.
* The example configuration file is configured to send several sets of stats to InfluxDB via the influxdb.go backend. If you intend to use the default backend, you will need to install InfluxDB. InfluxDB can be installed locally (i.e on the same system as the connector) or remotely (i.e. on a different system).

    ```sh
    sudo apt-get install influxdb
    ```

* If you installed InfluxDB to somewhere other than localhost and/or port 8086, then you'll also need to update the configuration file with the address and port of the InfluxDB service.
* To run the connector:

    ```sh
    ./gostats
    ```

## Customizing the connector

The connector is designed to allow for customization via a plugin architecture. The default plugin, influxdb.go, is configured via the provided example configuration file. If you would like to process the stats data differently or send them to a different backend than the influxdb.go you can implement a custom stats processor. Here are the instructions for doing so:

* Create a file called my_plugin.go, or whatever you want to name it.
* In the my_plugin.go file define the following:
  * a structure that retains the information needed for the stats-writing function to be able to send data to the backend. Influxdb example:

    ```go
    type InfluxDBSink struct {
        cluster  string
        c        client.Client
        bpConfig client.BatchPointsConfig
    }
    ```

  * a function with signature

    ```go
    func (s *InfluxDBSink) Init(cluster string, args []string) error
    ```

  that takes as input the name/ip-address of a cluster and a string array of backend-specific initialization parameters and initializes the receiver.
  * Also define a stat-writing function with the following signature:

    ```go
    func (s *InfluxDBSink) WriteStats(stats []StatResult) error
    ```

* Add the my_plugin.go file to the source directory.
* Add code to getDBWriter() in main.go to recognize your new backend.
* Update the idic.toml file with the name of your plugin (i.e. 'my_plugin')
* Rebuild and restart the gostats tool.
