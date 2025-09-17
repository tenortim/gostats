package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/op/go-logging"
)

func setMemoryBackend() {
	backend := logging.NewMemoryBackend(65536)
	logging.SetBackend(backend)

}

// Test DecodeStat with float64 value
func TestDecodeStat_Float64(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "cluster.net.ext.bytes.in.rate",
		Devid: 0,
		Value: float64(88920.0),
	}
	fields, tags, err := DecodeStat("clusterA", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 1 || len(tags) != 1 {
		t.Fatalf("expected 1 field/tag, got %d/%d", len(fields), len(tags))
	}
	if fields[0]["value"] != float64(88920.0) {
		t.Errorf("expected value 488920.0, got %v", fields[0]["value"])
	}
	if tags[0]["cluster"] != "clusterA" {
		t.Errorf("expected cluster tag 'clusterA', got %v", tags[0]["cluster"])
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
	fields, tags, err := DecodeStat("clusterB", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fields[0]["value"] != "someval" {
		t.Errorf("expected value 'someval', got %v", fields[0]["value"])
	}
	if tags[0]["cluster"] != "clusterB" || tags[0]["node"] != "1" {
		t.Errorf("expected clusterB/1, got %v", tags[0])
	}
}

// Test DecodeStat with array of maps (multi-valued stat)
func TestDecodeStat_ArrayOfMaps(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "node.ifs.heat.lock",
		Devid: 2,
		Value: []any{
			map[string]any{"op_rate": float64(131.6551361083984), "path": "SYSTEM (0x0)"},
			map[string]any{"op_rate": float64(60.76391220092773), "path": "/ifs/"},
		},
	}
	fields, tags, err := DecodeStat("clusterC", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 || len(tags) != 2 {
		t.Fatalf("expected 1 field and 3 tags, got %d/%d", len(fields), len(tags))
	}
	if fields[0]["op_rate"] != float64(131.6551361083984) || fields[1]["op_rate"] != float64(60.76391220092773) {
		t.Errorf("unexpected op_rate tags: %v, %v", tags[0]["op_rate"], tags[1]["op_rate"])
	}
	if tags[0]["path"] != "SYSTEM (0x0)" || tags[1]["path"] != "/ifs/" {
		t.Errorf("unexpected op_rate tags: %v, %v", tags[0]["path"], tags[1]["path"])
	}
	if tags[0]["cluster"] != "clusterC" || tags[0]["node"] != "2" {
		t.Errorf("expected clusterC/2, got %v", tags[0])
	}
}

// Test DecodeStat with map[string]any value
func TestDecodeStat_MapValue(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "node.mds.cache.stats",
		Devid: 2,
		Value: map[string]any{"hits": 5191200, "misses": 414440},
	}
	fields, tags, err := DecodeStat("clusterD", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fields[0]["hits"] != 5191200 {
		t.Errorf("expected hits count 5191200', got %v", fields[0]["hits"])
	}
	if fields[0]["misses"] != 414440 {
		t.Errorf("expected misses count 414440, got %v", fields[0]["misses"])
	}
	if len(tags) != 1 || tags[0]["cluster"] != "clusterD" || tags[0]["node"] != "2" {
		t.Errorf("expected clusterD/2, got %v", tags[0])
	}
}

// Test DecodeStat with nil value
func TestDecodeStat_NilValue(t *testing.T) {
	setMemoryBackend()
	stat := StatResult{
		Key:   "stat_error",
		Devid: 0,
		Value: nil,
	}
	fields, tags, err := DecodeStat("clusterE", stat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 0 || len(tags) != 0 {
		t.Errorf("expected no fields/tags for nil value, got %d/%d", len(fields), len(tags))
	}
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
	fields := ptFields{"op_name": "change_notify"}
	if !isInvalidStat(&fields) {
		t.Errorf("expected true for change_notify")
	}
	fields = ptFields{"op_name": "read_directory_change"}
	if !isInvalidStat(&fields) {
		t.Errorf("expected true for read_directory_change")
	}
	fields = ptFields{"op_name": "other"}
	if isInvalidStat(&fields) {
		t.Errorf("expected false for other")
	}
}

// Test ptTagmapCopy returns a true copy
func TestPtTagmapCopy(t *testing.T) {
	setMemoryBackend()
	orig := ptTags{"a": "1", "b": "2"}
	cp := ptTagmapCopy(orig)
	if !reflect.DeepEqual(orig, cp) {
		t.Errorf("expected copy to equal original")
	}
	cp["a"] = "changed"
	if orig["a"] == "changed" {
		t.Errorf("copy should not affect original")
	}
}
