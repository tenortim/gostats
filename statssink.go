package main

// DBWriter defines an interface to write OneFS stats to a persistent store/database
type DBWriter interface {
	// Initialize a statssink
	Init(clusterName string, config *tomlConfig, ci int, sg map[string]statDetail) error
	// Write a stat to the sink
	WriteStats(stats []StatResult) error
}
