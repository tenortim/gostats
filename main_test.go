package main

import (
	"sort"
	"testing"
	"time"
)

// testCluster returns a minimal Cluster sufficient for calcBuckets (only ClusterName is used).
func testCluster(name string) *Cluster {
	return &Cluster{ClusterName: name}
}

// statNames extracts and sorts the stat names from a slice of statTimeSets
// for deterministic comparison.
func allStatNames(buckets []statTimeSet) []string {
	var names []string
	for _, b := range buckets {
		names = append(names, b.stats...)
	}
	sort.Strings(names)
	return names
}

// TestCalcBuckets_FetchByStatgroup_False_MergesGroups verifies that two stat groups
// with the same absolute interval are merged into a single bucket when
// fetch_by_statgroup is false (the default).
func TestCalcBuckets_FetchByStatgroup_False_MergesGroups(t *testing.T) {
	sg := map[string]statGroup{
		"groupA": {sgRefresh{0, 30}, []string{"stat.a1", "stat.a2"}},
		"groupB": {sgRefresh{0, 30}, []string{"stat.b1", "stat.b2"}},
	}
	sd := map[string]statDetail{
		"stat.a1": {valid: true, updateIntvl: 30},
		"stat.a2": {valid: true, updateIntvl: 30},
		"stat.b1": {valid: true, updateIntvl: 30},
		"stat.b2": {valid: true, updateIntvl: 30},
	}

	buckets := calcBuckets(testCluster("test"), 5, sg, sd, false)

	if len(buckets) != 1 {
		t.Fatalf("expected 1 merged bucket, got %d", len(buckets))
	}
	if buckets[0].groupName != "" {
		t.Errorf("expected empty groupName in batched mode, got %q", buckets[0].groupName)
	}
	if buckets[0].interval != 30*time.Second {
		t.Errorf("expected 30s interval, got %v", buckets[0].interval)
	}
	got := allStatNames(buckets)
	want := []string{"stat.a1", "stat.a2", "stat.b1", "stat.b2"}
	if len(got) != len(want) {
		t.Errorf("expected stats %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected stat %q at index %d, got %q", want[i], i, got[i])
		}
	}
}

// TestCalcBuckets_FetchByStatgroup_True_IsolatesGroups verifies that two stat groups
// with the same absolute interval are kept in separate buckets when
// fetch_by_statgroup is true.
func TestCalcBuckets_FetchByStatgroup_True_IsolatesGroups(t *testing.T) {
	sg := map[string]statGroup{
		"groupA": {sgRefresh{0, 30}, []string{"stat.a1", "stat.a2"}},
		"groupB": {sgRefresh{0, 30}, []string{"stat.b1", "stat.b2"}},
	}
	sd := map[string]statDetail{
		"stat.a1": {valid: true, updateIntvl: 30},
		"stat.a2": {valid: true, updateIntvl: 30},
		"stat.b1": {valid: true, updateIntvl: 30},
		"stat.b2": {valid: true, updateIntvl: 30},
	}

	buckets := calcBuckets(testCluster("test"), 5, sg, sd, true)

	if len(buckets) != 2 {
		t.Fatalf("expected 2 isolated buckets, got %d", len(buckets))
	}
	for _, b := range buckets {
		if b.groupName == "" {
			t.Error("expected non-empty groupName in per-group mode")
		}
		if b.interval != 30*time.Second {
			t.Errorf("expected 30s interval, got %v", b.interval)
		}
		if len(b.stats) != 2 {
			t.Errorf("expected 2 stats per bucket, got %d in group %q", len(b.stats), b.groupName)
		}
	}
	// All four stats should still be present across both buckets
	got := allStatNames(buckets)
	want := []string{"stat.a1", "stat.a2", "stat.b1", "stat.b2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected stat %q at index %d, got %q", want[i], i, got[i])
		}
	}
}

// TestCalcBuckets_FetchByStatgroup_True_MultiplierGroups verifies per-group isolation
// when using multiplier-based intervals with statDetail metadata.
func TestCalcBuckets_FetchByStatgroup_True_MultiplierGroups(t *testing.T) {
	// Both groups use 1x multiplier; stats have the same native interval,
	// so without isolation they would merge into one bucket.
	sg := map[string]statGroup{
		"cpu":  {sgRefresh{1.0, 0}, []string{"cluster.cpu.user", "cluster.cpu.sys"}},
		"disk": {sgRefresh{1.0, 0}, []string{"cluster.disk.reads", "cluster.disk.writes"}},
	}
	sd := map[string]statDetail{
		"cluster.cpu.user":    {valid: true, updateIntvl: 30},
		"cluster.cpu.sys":     {valid: true, updateIntvl: 30},
		"cluster.disk.reads":  {valid: true, updateIntvl: 30},
		"cluster.disk.writes": {valid: true, updateIntvl: 30},
	}

	buckets := calcBuckets(testCluster("test"), 5, sg, sd, true)

	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets (one per group), got %d", len(buckets))
	}
	groupNames := make(map[string]bool)
	for _, b := range buckets {
		groupNames[b.groupName] = true
	}
	if !groupNames["cpu"] || !groupNames["disk"] {
		t.Errorf("expected group names 'cpu' and 'disk', got %v", groupNames)
	}
}

// TestCalcBuckets_FetchByStatgroup_True_DifferentIntervals verifies that within a
// single group, stats with different computed intervals still produce separate
// buckets (one per distinct interval within the group).
func TestCalcBuckets_FetchByStatgroup_True_DifferentIntervals(t *testing.T) {
	sg := map[string]statGroup{
		"mixed": {sgRefresh{1.0, 0}, []string{"fast.stat", "slow.stat"}},
	}
	sd := map[string]statDetail{
		"fast.stat": {valid: true, updateIntvl: 10},
		"slow.stat": {valid: true, updateIntvl: 60},
	}

	buckets := calcBuckets(testCluster("test"), 5, sg, sd, true)

	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets for 2 distinct intervals, got %d", len(buckets))
	}
	for _, b := range buckets {
		if b.groupName != "mixed" {
			t.Errorf("expected groupName 'mixed', got %q", b.groupName)
		}
		if len(b.stats) != 1 {
			t.Errorf("expected 1 stat per interval bucket, got %d", len(b.stats))
		}
	}
}

// TestCalcBuckets_InvalidStatsSkipped verifies that invalid stats are omitted
// regardless of the fetchByStatgroup setting.
func TestCalcBuckets_InvalidStatsSkipped(t *testing.T) {
	sg := map[string]statGroup{
		"groupA": {sgRefresh{1.0, 0}, []string{"good.stat", "bad.stat"}},
	}
	sd := map[string]statDetail{
		"good.stat": {valid: true, updateIntvl: 30},
		"bad.stat":  {valid: false, updateIntvl: 30},
	}

	for _, fetchByGroup := range []bool{false, true} {
		buckets := calcBuckets(testCluster("test"), 5, sg, sd, fetchByGroup)
		names := allStatNames(buckets)
		for _, n := range names {
			if n == "bad.stat" {
				t.Errorf("fetchByStatgroup=%v: invalid stat 'bad.stat' should have been skipped", fetchByGroup)
			}
		}
		found := false
		for _, n := range names {
			if n == "good.stat" {
				found = true
			}
		}
		if !found {
			t.Errorf("fetchByStatgroup=%v: valid stat 'good.stat' was unexpectedly absent", fetchByGroup)
		}
	}
}
