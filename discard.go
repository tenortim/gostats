package main

// DiscardSink defines the data for the null/discard back end
type DiscardSink struct {
	cluster string
}

// GetDiscardWriter returns a discard DBWriter
func GetDiscardWriter() DBWriter {
	return &DiscardSink{}
}

// Init initializes an DiscardSink so that points can be written (thrown away)
func (s *DiscardSink) Init(clusterName string, _ *tomlConfig, _ int, _ map[string]statDetail) error {
	s.cluster = clusterName
	return nil
}

// WriteStats takes an array of StatResults and discards them
func (s *DiscardSink) WriteStats(stats []StatResult) error {
	// consider debug/trace statement here for stat count
	return nil
}
