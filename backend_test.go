package main

import (
	"errors"
	"testing"

	"github.com/op/go-logging"
)

// setMemoryBackend sets the logging backend to an in-memory backend for testing
func setMemoryBackend() {
	backend := logging.NewMemoryBackend(65536)
	logging.SetBackend(backend)

}

// DecodeStat tests
// DecodeStat returns and array of field maps and an array of tag maps
// Each field map corresponds to a tag map by index
// For single-valued stats, there will be one field map and one tag map
// For multi-valued stats, there will be multiple field maps and multiple tag maps
// The length of the field and tag arrays will be the same
// DecodeStat has special logic to remove certain stats that are not useful
// (e.g. change_notify and read_directory_change) - these are tested in TestIsInvalidStat

// Test DecodeStat with float64 value
func TestDecodeStat_Float64(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "cluster.net.ext.bytes.in.rate",
		Devid: 0,
		Value: float64(88920.0),
	}
	fa, ta, err := DecodeStat("clusterA", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 1 || len(ta) != 1 {
		t.Fatalf("expected 1 set of fields and tags, got %d/%d", len(fa), len(ta))
	}
	if len(fa[0]) != 1 || len(ta[0]) != 1 {
		t.Fatalf("expected 1 field and 1 tag, got %d/%d", len(fa[0]), len(ta[0]))
	}
	if fa[0]["value"] != float64(88920.0) {
		t.Errorf("expected value 488920.0, got %v", fa[0]["value"])
	}
	if ta[0]["cluster"] != "clusterA" {
		t.Errorf("expected cluster tag 'clusterA', got %v", ta[0]["cluster"])
	}
}

// Test DecodeStat with string value
// This will need to change once DecodeStat is changed to return an error for string values
func TestDecodeStat_String(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "no_such_stat",
		Devid: 1,
		Value: "someval",
	}
	_, _, err := DecodeStat("clusterB", stat)
	if err == nil {
		t.Fatalf("expected error but got none")
	}
}

// intPtr is a helper function to return a pointer to an int
func intPtr(i int) *int {
	return &i
}

// Test DecodeStat with sing-entry array of maps (multi-valued stat)
func TestDecodeStat_ShortArrayOfMaps(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "node.ifs.heat.lock",
		Devid: 3,
		Node:  intPtr(3),
		Value: []any{
			map[string]any{"op_rate": float64(131.6551361083984), "path": "SYSTEM (0x0)"},
		},
	}
	fa, ta, err := DecodeStat("clusterC", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 1 || len(ta) != 1 {
		t.Fatalf("expected 1 set of fields and tags, got %d/%d", len(fa), len(ta))
	}
	if len(fa[0]) != 1 || len(ta[0]) != 4 {
		t.Fatalf("expected 1 field and 4 tags, got %d/%d", len(fa[0]), len(ta[0]))
	}
	if fa[0]["op_rate"] != float64(131.6551361083984) {
		t.Errorf("unexpected op_rate value: %v", fa[0]["op_rate"])
	}
	if ta[0]["path"] != "SYSTEM (0x0)" {
		t.Errorf("unexpected op_rate tag: %v", ta[0]["path"])
	}
	if ta[0]["cluster"] != "clusterC" || ta[0]["node"] != "3" {
		t.Errorf("expected clusterC/2, got %v", ta[0])
	}
}

// Test DecodeStat with empty array
func TestDecodeStat_EmptyArray(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "stat_empty_array",
		Devid: 0,
		Value: []any{},
	}
	fa, ta, err := DecodeStat("clusterE", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 0 || len(ta) != 0 {
		t.Fatalf("expected 0 sets of fields and tags, got %d/%d", len(fa), len(ta))
	}
}

// Test DecodeStat with array of maps (multi-valued stat)
func TestDecodeStat_ArrayOfMaps(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "node.ifs.heat.lock",
		Devid: 16,
		Node:  intPtr(2),
		Value: []any{
			map[string]any{"op_rate": float64(131.6551361083984), "path": "SYSTEM (0x0)"},
			map[string]any{"op_rate": float64(60.76391220092773), "path": "/ifs/"},
		},
	}
	fa, ta, err := DecodeStat("clusterC", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 2 || len(ta) != 2 {
		t.Fatalf("expected 2 sets of fields and tags, got %d/%d", len(fa), len(ta))
	}
	if len(fa[0]) != 1 || len(ta[0]) != 4 || len(fa[1]) != 1 || len(ta[1]) != 4 {
		t.Fatalf("expected 1 field and 4 tags, got %d/%d, %d/%d", len(fa[0]), len(ta[0]), len(fa[1]), len(ta[1]))
	}
	if fa[0]["op_rate"] != float64(131.6551361083984) || fa[1]["op_rate"] != float64(60.76391220092773) {
		t.Errorf("unexpected op_rate values: %v, %v", fa[0]["op_rate"], fa[1]["op_rate"])
	}
	if ta[0]["path"] != "SYSTEM (0x0)" || ta[1]["path"] != "/ifs/" {
		t.Errorf("unexpected op_rate tags: %v, %v", ta[0]["path"], ta[1]["path"])
	}
	if ta[0]["cluster"] != "clusterC" || ta[0]["node"] != "2" {
		t.Errorf("expected clusterC/2, got %v", ta[0])
	}
}

// Test DecodeStat with array of maps (multi-valued stat) where the map contains an array
func TestDecodeStat_ArrayOfMaps_with_Array(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "node.ifs.heat.lock",
		Devid: 1,
		Node:  intPtr(1),
		Value: []any{
			map[string]any{
				"client_id":  1000,
				"local_addr": "192.168.0.112",
				"op_class_values": []any{
					map[string]any{"class_name": "write", "in_max": 1047568, "in_min": 849, "in_rate": 2684809.250},
					map[string]any{"class_name": "namespace_read", "in_max": 138, "in_min": 8, "in_rate": 9742.5996093750},
					map[string]any{"class_name": "namespace_write", "in_max": 187, "in_min": 40, "in_rate": 5403.600097656250},
					map[string]any{"class_name": "file_state", "in_max": 143, "in_min": 116, "in_rate": 3063.600097656250},
					map[string]any{"class_name": "other", "in_max": 135, "in_min": 135, "in_rate": 54.0},
				},
				"remote_addr": "192.168.1.6",
			},
			map[string]any{
				"client_id":  1001,
				"local_addr": "192.168.0.112",
				"op_class_values": []any{
					map[string]any{"class_name": "namespace_read", "in_max": 2537, "in_min": 2368, "in_rate": 12345.67},
				},
				"remote_addr": "192.168.1.7",
			},
		},
	}
	fa, ta, err := DecodeStat("clusterC", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 6 || len(ta) != 6 {
		t.Fatalf("expected 6 sets of fields and tags, got %d/%d", len(fa), len(ta))
	}
	for i := range fa {
		if len(fa[i]) != 4 || len(ta[i]) != 6 {
			t.Fatalf("field set %v: expected 4 fields and 6 tags, got %d/%d", i, len(fa[i]), len(ta[i]))
		}
		if ta[i]["cluster"] != "clusterC" || ta[i]["node"] != "1" {
			t.Errorf("tag set %v: expected clusterC/1, got %v", i, ta[i])
		}
	}
	if fa[0]["client_id"] != 1000 || fa[1]["client_id"] != 1000 || fa[2]["client_id"] != 1000 {
		t.Errorf("unexpected client_id values in first 3 field sets: %v, %v, %v", fa[0]["client_id"], fa[1]["client_id"], fa[2]["client_id"])
	}
	if fa[3]["client_id"] != 1000 || fa[4]["client_id"] != 1000 || fa[5]["client_id"] != 1001 {
		t.Errorf("unexpected client_id values in last 3 field sets: %v, %v, %v", fa[3]["client_id"], fa[4]["client_id"], fa[5]["client_id"])
	}
	if fa[4]["in_max"] != 135 || fa[5]["in_max"] != 2537 {
		t.Errorf("unexpected in_max values in last 2 field sets: %v, %v", fa[4]["in_max"], fa[5]["in_max"])
	}
	if ta[0]["local_addr"] != "192.168.0.112" {
		t.Errorf("unexpected local_addr in first tag set: %v", ta[0]["local_addr"])
	}
	if ta[0]["remote_addr"] != "192.168.1.6" || ta[5]["remote_addr"] != "192.168.1.7" {
		t.Errorf("unexpected remote_addr in first and last tag sets: %v, %v", ta[0]["remote_addr"], ta[5]["remote_addr"])
	}
}

// Test DecodeStat with map[string]any value
func TestDecodeStat_SimpleMap(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "node.mds.cache.stats",
		Devid: 5,
		Node:  intPtr(5),
		Value: map[string]any{"hits": 5191200, "misses": 414440},
	}
	fa, ta, err := DecodeStat("clusterD", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 1 || len(ta) != 1 {
		t.Fatalf("expected 1 set of fields and tags, got %d/%d", len(fa), len(ta))
	}
	if len(fa[0]) != 2 || len(ta[0]) != 3 {
		t.Fatalf("expected 2 fields and 3 tags, got %d/%d", len(fa[0]), len(ta[0]))
	}
	if fa[0]["hits"] != 5191200 {
		t.Errorf("expected hits count 5191200', got %v", fa[0]["hits"])
	}
	if fa[0]["misses"] != 414440 {
		t.Errorf("expected misses count 414440, got %v", fa[0]["misses"])
	}
	if ta[0]["cluster"] != "clusterD" || ta[0]["node"] != "5" {
		t.Errorf("expected clusterD/2, got %v", ta[0])
	}
}

// Test DecodeStat with map containing an array of maps (multi-valued stat)
func TestDecodeStat_Map_with_Array_of_Maps(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "stat_with_array",
		Devid: 0,
		Value: map[string]any{
			"count": 42,
			"items": []any{
				map[string]any{"name": "item1", "value": 100},
				map[string]any{"name": "item2", "value": 200},
			},
		},
	}
	fa, ta, err := DecodeStat("clusterG", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 2 || len(ta) != 2 {
		t.Fatalf("expected 2 sets of fields and tags, got %d/%d", len(fa), len(ta))
	}
	if len(fa[0]) != 2 || len(ta[0]) != 2 || len(fa[1]) != 2 || len(ta[1]) != 2 {
		t.Fatalf("expected 2 fields and 2 tags, got %d/%d, %d/%d", len(fa[0]), len(ta[0]), len(fa[1]), len(ta[1]))
	}
	if fa[0]["count"] != 42 || fa[1]["count"] != 42 {
		t.Errorf("expected count 42, got %v, %v", fa[0]["count"], fa[1]["count"])
	}
	if fa[0]["value"] != 100 || fa[1]["value"] != 200 {
		t.Errorf("expected values 100, 200, got %v, %v", fa[0]["value"], fa[1]["value"])
	}
	if ta[0]["cluster"] != "clusterG" || ta[1]["cluster"] != "clusterG" {
		t.Errorf("expected clusterG tags, got %v and %v", ta[0], ta[1])
	}
	if ta[0]["name"] != "item1" || ta[1]["name"] != "item2" {
		t.Errorf("expected tags \"item1\" and \"item2\", got %v, %v", ta[0]["name"], ta[1]["name"])
	}
}

// Test elision of certain stats (change_notify and read_directory_change)
func TestDecodeStat_SMB_Elision(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "cluster.protostats.smb2",
		Devid: 0,
		Value: []any{
			map[string]any{"op_name": "change_notify", "op_rate": 12.6},
			map[string]any{"op_name": "read", "op_rate": 3456.1},
			map[string]any{"op_name": "write", "op_rate": 789.2},
		},
	}
	fa, ta, err := DecodeStat("clusterH", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 2 || len(ta) != 2 {
		t.Logf("expected 2 sets of fields and tags, got %d/%d", len(fa), len(ta))
		t.Fatalf("tags: %#v", ta)
	}
	if len(fa[0]) != 1 || len(ta[0]) != 2 || len(fa[1]) != 1 || len(ta[1]) != 2 {
		t.Fatalf("expected 1 field and 2 tags, got %d/%d, %d/%d", len(fa[0]), len(ta[0]), len(fa[1]), len(ta[1]))
	}
	if fa[0]["op_rate"] != 3456.1 || fa[1]["op_rate"] != 789.2 {
		t.Errorf("unexpected op_rate values: %v, %v", fa[0]["op_rate"], fa[1]["op_rate"])
	}
	if ta[0]["op_name"] != "read" || ta[1]["op_name"] != "write" {
		t.Errorf("unexpected op_name tags: %v, %v", ta[0]["op_name"], ta[1]["op_name"])
	}
	if ta[0]["cluster"] != "clusterH" {
		t.Errorf("expected clusterH, got %v", ta[0])
	}
}

// Test elision of certain stats (change_notify and read_directory_change)
func TestDecodeStat_IRP_Elision(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "cluster.protostats.smb2",
		Devid: 0,
		Value: []any{
			map[string]any{"op_name": "read_directory_change", "op_rate": 12.6},
			map[string]any{"op_name": "read", "op_rate": 3456.1},
			map[string]any{"op_name": "write", "op_rate": 789.2},
		},
	}
	fa, ta, err := DecodeStat("clusterI", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fa) != 2 || len(ta) != 2 {
		t.Logf("expected 2 sets of fields and tags, got %d/%d", len(fa), len(ta))
		t.Fatalf("tags: %#v", ta)
	}
	if len(fa[0]) != 1 || len(ta[0]) != 2 || len(fa[1]) != 1 || len(ta[1]) != 2 {
		t.Fatalf("expected 1 field and 2 tags, got %d/%d, %d/%d", len(fa[0]), len(ta[0]), len(fa[1]), len(ta[1]))
	}
	if fa[0]["op_rate"] != 3456.1 || fa[1]["op_rate"] != 789.2 {
		t.Errorf("unexpected op_rate values: %v, %v", fa[0]["op_rate"], fa[1]["op_rate"])
	}
	if ta[0]["op_name"] != "read" || ta[1]["op_name"] != "write" {
		t.Errorf("unexpected op_name tags: %v, %v", ta[0]["op_name"], ta[1]["op_name"])
	}
	if ta[0]["cluster"] != "clusterI" {
		t.Errorf("expected clusterI, got %v", ta[0])
	}
}

// Test DecodeStat with nil value
func TestDecodeStat_NilValue(t *testing.T) {
	setMemoryBackend()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for nil value")
		}
	}()
	stat := StatResult{
		Key:   "stat_error",
		Devid: 0,
		Value: nil,
	}
	DecodeStat("clusterE", stat)
}

// Test DecodeStat with unknown value type (should panic)
func TestDecodeStat_UnknownType(t *testing.T) {
	setMemoryBackend()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for unknown value type")
		}
	}()
	stat := StatResult{
		Key:   "stat6",
		Devid: 0,
		Value: errors.New("bad type"),
	}
	DecodeStat("clusterF", stat)
}

// Test isInvalidStat for change_notify and read_directory_change
func TestIsInvalidStat(t *testing.T) {
	setMemoryBackend()
	tags := ptTags{"op_name": "change_notify"}
	if !isInvalidStat(&tags) {
		t.Errorf("expected true for change_notify")
	}
	tags = ptTags{"op_name": "read_directory_change"}
	if !isInvalidStat(&tags) {
		t.Errorf("expected true for read_directory_change")
	}
	tags = ptTags{"op_name": "other"}
	if isInvalidStat(&tags) {
		t.Errorf("expected false for other")
	}
}
