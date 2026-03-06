package main

import (
	"testing"
)

// int64Ptr is a helper to get a pointer to an int64
func int64Ptr(i int64) *int64 {
	return &i
}

// Tests for decodeProtocolSummaryStat

func TestDecodeProtocolSummaryStat_WithNode(t *testing.T) {
	setMemoryBackend()
	node := int64(3)
	item := SummaryStatsProtocolItem{
		Class:           "read",
		Node:            &node,
		Operation:       "nfs3_read",
		Protocol:        "nfs3",
		In:              1024.0,
		InAvg:           512.0,
		InMax:           2048.0,
		InMin:           64.0,
		InStandardDev:   128.0,
		OperationCount:  100,
		OperationRate:   10.5,
		Out:             2048.0,
		OutAvg:          1024.0,
		OutMax:          4096.0,
		OutMin:          128.0,
		OutStandardDev:  256.0,
		Time:            1700000000,
		TimeAvg:         500.0,
		TimeMax:         2000.0,
		TimeMin:         100.0,
		TimeStandardDev: 300.0,
	}
	fields, tags := decodeProtocolSummaryStat("clusterA", item)

	// 5 tags: cluster, class, operation, protocol, node
	if len(tags) != 5 {
		t.Errorf("expected 5 tags, got %d: %v", len(tags), tags)
	}
	if tags["cluster"] != "clusterA" {
		t.Errorf("expected cluster tag 'clusterA', got %q", tags["cluster"])
	}
	if tags["node"] != "3" {
		t.Errorf("expected node tag '3', got %q", tags["node"])
	}
	if tags["class"] != "read" {
		t.Errorf("expected class tag 'read', got %q", tags["class"])
	}
	if tags["operation"] != "nfs3_read" {
		t.Errorf("expected operation tag 'nfs3_read', got %q", tags["operation"])
	}
	if tags["protocol"] != "nfs3" {
		t.Errorf("expected protocol tag 'nfs3', got %q", tags["protocol"])
	}

	// 17 fields
	if len(fields) != 17 {
		t.Errorf("expected 17 fields, got %d: %v", len(fields), fields)
	}
	if fields["in"] != float64(1024.0) {
		t.Errorf("unexpected in: %v", fields["in"])
	}
	if fields["operation_count"] != int64(100) {
		t.Errorf("unexpected operation_count: %v", fields["operation_count"])
	}
	if fields["time"] != int64(1700000000) {
		t.Errorf("unexpected time: %v", fields["time"])
	}
}

func TestDecodeProtocolSummaryStat_WithoutNode(t *testing.T) {
	setMemoryBackend()
	item := SummaryStatsProtocolItem{
		Class:     "write",
		Node:      nil,
		Operation: "smb2_write",
		Protocol:  "smb2",
	}
	fields, tags := decodeProtocolSummaryStat("clusterB", item)

	// 4 tags: cluster, class, operation, protocol (no node)
	if len(tags) != 4 {
		t.Errorf("expected 4 tags, got %d: %v", len(tags), tags)
	}
	if _, ok := tags["node"]; ok {
		t.Errorf("expected no node tag, but got %q", tags["node"])
	}
	if len(fields) != 17 {
		t.Errorf("expected 17 fields, got %d", len(fields))
	}
}

// Tests for decodeClientSummaryStat

func TestDecodeClientSummaryStat_WithNodeAndUser(t *testing.T) {
	setMemoryBackend()
	node := int64(2)
	item := SummaryStatsClientItem{
		Class:         "nfs",
		Node:          &node,
		In:            500.0,
		InAvg:         250.0,
		InMax:         1000.0,
		InMin:         50.0,
		LocalAddr:     "10.0.0.1",
		LocalName:     "node2.example.com",
		NumOperations: 200,
		OperationRate: 20.0,
		Out:           1000.0,
		OutAvg:        500.0,
		OutMax:        2000.0,
		OutMin:        100.0,
		Protocol:      "nfs3",
		RemoteAddr:    "192.168.1.50",
		RemoteName:    "client.example.com",
		Time:          1700000001,
		TimeAvg:       400.0,
		TimeMax:       1600.0,
		TimeMin:       80.0,
		User: &struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		}{ID: "UID:1000", Name: "alice", Type: "user"},
	}
	fields, tags := decodeClientSummaryStat("clusterC", item)

	// 11 tags: cluster, class, local_addr, local_name, protocol, remote_addr, remote_name, node, user_id, user_name, user_type
	if len(tags) != 11 {
		t.Errorf("expected 11 tags, got %d: %v", len(tags), tags)
	}
	if tags["node"] != "2" {
		t.Errorf("expected node tag '2', got %q", tags["node"])
	}
	if tags["user_id"] != "UID:1000" {
		t.Errorf("expected user_id 'UID:1000', got %q", tags["user_id"])
	}
	if tags["user_name"] != "alice" {
		t.Errorf("expected user_name 'alice', got %q", tags["user_name"])
	}
	if tags["user_type"] != "user" {
		t.Errorf("expected user_type 'user', got %q", tags["user_type"])
	}

	// 14 fields: in, in_avg, in_max, in_min, num_operations, operation_rate, out, out_avg, out_max, out_min, time, time_avg, time_max, time_min
	if len(fields) != 14 {
		t.Errorf("expected 14 fields, got %d: %v", len(fields), fields)
	}
	if fields["num_operations"] != int64(200) {
		t.Errorf("unexpected num_operations: %v", fields["num_operations"])
	}
	if fields["time"] != int64(1700000001) {
		t.Errorf("unexpected time: %v", fields["time"])
	}
}

func TestDecodeClientSummaryStat_WithoutNodeOrUser(t *testing.T) {
	setMemoryBackend()
	item := SummaryStatsClientItem{
		Class:      "smb",
		Node:       nil,
		LocalAddr:  "10.0.0.2",
		LocalName:  "node3.example.com",
		Protocol:   "smb2",
		RemoteAddr: "192.168.1.51",
		RemoteName: "winclient.example.com",
		User:       nil,
	}
	fields, tags := decodeClientSummaryStat("clusterD", item)

	// 7 tags: cluster, class, local_addr, local_name, protocol, remote_addr, remote_name
	if len(tags) != 7 {
		t.Errorf("expected 7 tags, got %d: %v", len(tags), tags)
	}
	if _, ok := tags["node"]; ok {
		t.Errorf("expected no node tag, but got %q", tags["node"])
	}
	if _, ok := tags["user_id"]; ok {
		t.Errorf("expected no user_id tag, but got one")
	}
	if len(fields) != 14 {
		t.Errorf("expected 14 fields, got %d", len(fields))
	}
}

// Tests for decodeDriveSummaryStat

func TestDecodeDriveSummaryStat(t *testing.T) {
	setMemoryBackend()
	item := SummaryStatsDriveItem{
		DriveID:          "1:5",
		Type:             "ssd",
		AccessLatency:    1.5,
		AccessSlow:       0.1,
		Busy:             42.3,
		BytesIn:          102400.0,
		BytesOut:         204800.0,
		IoschedLatency:   0.5,
		IoschedQueue:     2.0,
		Time:             1700000002,
		UsedBytesPercent: 67.8,
		UsedInodes:       123456.0,
		XferSizeIn:       4096.0,
		XferSizeOut:      8192.0,
		XfersIn:          25.0,
		XfersOut:         50.0,
	}
	fields, tags := decodeDriveSummaryStat("clusterE", item)

	// 3 tags: cluster, drive_id, type
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags["cluster"] != "clusterE" {
		t.Errorf("expected cluster tag 'clusterE', got %q", tags["cluster"])
	}
	if tags["drive_id"] != "1:5" {
		t.Errorf("expected drive_id tag '1:5', got %q", tags["drive_id"])
	}
	if tags["type"] != "ssd" {
		t.Errorf("expected type tag 'ssd', got %q", tags["type"])
	}

	// 14 fields
	if len(fields) != 14 {
		t.Errorf("expected 14 fields, got %d: %v", len(fields), fields)
	}
	if fields["busy"] != float64(42.3) {
		t.Errorf("unexpected busy: %v", fields["busy"])
	}
	if fields["bytes_in"] != float64(102400.0) {
		t.Errorf("unexpected bytes_in: %v", fields["bytes_in"])
	}
	if fields["used_bytes_percent"] != float64(67.8) {
		t.Errorf("unexpected used_bytes_percent: %v", fields["used_bytes_percent"])
	}
	if fields["time"] != int64(1700000002) {
		t.Errorf("unexpected time: %v", fields["time"])
	}
}

// Tests for Unmarshal functions

func TestUnmarshalSummaryStatsProtocol_Valid(t *testing.T) {
	data := []byte(`{"protocol":[{"class":"read","node":1,"operation":"nfs3_read","protocol":"nfs3","in":1024.0,"in_avg":512.0,"in_max":2048.0,"in_min":64.0,"in_standard_dev":128.0,"operation_count":100,"operation_rate":10.5,"out":2048.0,"out_avg":1024.0,"out_max":4096.0,"out_min":128.0,"out_standard_dev":256.0,"time":1700000000,"time_avg":500.0,"time_max":2000.0,"time_min":100.0,"time_standard_dev":300.0}]}`)
	r, err := UnmarshalSummaryStatsProtocol(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Protocol) != 1 {
		t.Fatalf("expected 1 protocol item, got %d", len(r.Protocol))
	}
	if r.Protocol[0].Operation != "nfs3_read" {
		t.Errorf("expected operation 'nfs3_read', got %q", r.Protocol[0].Operation)
	}
	if r.Protocol[0].Node == nil || *r.Protocol[0].Node != 1 {
		t.Errorf("expected node 1, got %v", r.Protocol[0].Node)
	}
	if len(r.Errors) != 0 {
		t.Errorf("expected no errors, got %v", r.Errors)
	}
}

func TestUnmarshalSummaryStatsProtocol_Error(t *testing.T) {
	data := []byte(`{"errors":[{"code":"AEC_FORBIDDEN","message":"Access denied"}]}`)
	r, err := UnmarshalSummaryStatsProtocol(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(r.Errors))
	}
	if r.Errors[0].Code != "AEC_FORBIDDEN" {
		t.Errorf("expected error code 'AEC_FORBIDDEN', got %q", r.Errors[0].Code)
	}
	if len(r.Protocol) != 0 {
		t.Errorf("expected no protocol items, got %d", len(r.Protocol))
	}
}

func TestUnmarshalSummaryStatsProtocol_InvalidJSON(t *testing.T) {
	_, err := UnmarshalSummaryStatsProtocol([]byte(`not json`))
	if err == nil {
		t.Errorf("expected error for invalid JSON, got none")
	}
}

func TestUnmarshalSummaryStatsClient_Valid(t *testing.T) {
	data := []byte(`{"client":[{"class":"nfs","in":500.0,"in_avg":250.0,"in_max":1000.0,"in_min":50.0,"local_addr":"10.0.0.1","local_name":"node1","node":2,"num_operations":200,"operation_rate":20.0,"out":1000.0,"out_avg":500.0,"out_max":2000.0,"out_min":100.0,"protocol":"nfs3","remote_addr":"192.168.1.50","remote_name":"client1","time":1700000001,"time_avg":400.0,"time_max":1600.0,"time_min":80.0}]}`)
	r, err := UnmarshalSummaryStatsClient(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Client) != 1 {
		t.Fatalf("expected 1 client item, got %d", len(r.Client))
	}
	if r.Client[0].RemoteName != "client1" {
		t.Errorf("expected remote_name 'client1', got %q", r.Client[0].RemoteName)
	}
	if r.Client[0].Node == nil || *r.Client[0].Node != 2 {
		t.Errorf("expected node 2, got %v", r.Client[0].Node)
	}
}

func TestUnmarshalSummaryStatsClient_Error(t *testing.T) {
	data := []byte(`{"errors":[{"code":"AEC_NOT_FOUND","message":"Resource not found"}]}`)
	r, err := UnmarshalSummaryStatsClient(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(r.Errors))
	}
	if r.Errors[0].Code != "AEC_NOT_FOUND" {
		t.Errorf("expected error code 'AEC_NOT_FOUND', got %q", r.Errors[0].Code)
	}
	if len(r.Client) != 0 {
		t.Errorf("expected no client items, got %d", len(r.Client))
	}
}

func TestUnmarshalSummaryStatsClient_InvalidJSON(t *testing.T) {
	_, err := UnmarshalSummaryStatsClient([]byte(`{bad json`))
	if err == nil {
		t.Errorf("expected error for invalid JSON, got none")
	}
}

func TestUnmarshalSummaryStatsDrive_Valid(t *testing.T) {
	data := []byte(`{"drive":[{"access_latency":1.5,"access_slow":0.1,"busy":42.3,"bytes_in":102400.0,"bytes_out":204800.0,"drive_id":"1:5","iosched_latency":0.5,"iosched_queue":2.0,"time":1700000002,"type":"ssd","used_bytes_percent":67.8,"used_inodes":123456.0,"xfer_size_in":4096.0,"xfer_size_out":8192.0,"xfers_in":25.0,"xfers_out":50.0}]}`)
	r, err := UnmarshalSummaryStatsDrive(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Drive) != 1 {
		t.Fatalf("expected 1 drive item, got %d", len(r.Drive))
	}
	if r.Drive[0].DriveID != "1:5" {
		t.Errorf("expected drive_id '1:5', got %q", r.Drive[0].DriveID)
	}
	if r.Drive[0].Type != "ssd" {
		t.Errorf("expected type 'ssd', got %q", r.Drive[0].Type)
	}
	if r.Drive[0].Busy != 42.3 {
		t.Errorf("expected busy 42.3, got %v", r.Drive[0].Busy)
	}
	if r.Drive[0].Time != 1700000002 {
		t.Errorf("expected time 1700000002, got %v", r.Drive[0].Time)
	}
	if len(r.Errors) != 0 {
		t.Errorf("expected no errors, got %v", r.Errors)
	}
}

func TestUnmarshalSummaryStatsDrive_Error(t *testing.T) {
	data := []byte(`{"errors":[{"code":"AEC_FORBIDDEN","message":"Access denied"}]}`)
	r, err := UnmarshalSummaryStatsDrive(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(r.Errors))
	}
	if r.Errors[0].Code != "AEC_FORBIDDEN" {
		t.Errorf("expected error code 'AEC_FORBIDDEN', got %q", r.Errors[0].Code)
	}
	if len(r.Drive) != 0 {
		t.Errorf("expected no drive items, got %d", len(r.Drive))
	}
}

func TestUnmarshalSummaryStatsDrive_InvalidJSON(t *testing.T) {
	_, err := UnmarshalSummaryStatsDrive([]byte(`]not json[`))
	if err == nil {
		t.Errorf("expected error for invalid JSON, got none")
	}
}

func TestUnmarshalSummaryStatsDrive_Empty(t *testing.T) {
	data := []byte(`{"drive":[]}`)
	r, err := UnmarshalSummaryStatsDrive(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Drive) != 0 {
		t.Errorf("expected 0 drive items, got %d", len(r.Drive))
	}
}
