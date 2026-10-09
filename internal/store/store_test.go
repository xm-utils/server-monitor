package store

import (
	"testing"

	"server_monitor_service/internal/model"
)

func sysSnap(ts int64) model.SystemSnapshot {
	s := model.SystemSnapshot{Timestamp: ts}
	s.CPU.UsageTotal = float64(ts)
	return s
}

func TestRingPushLatestHistory(t *testing.T) {
	r := newRing[model.SystemSnapshot](3)

	if _, ok := r.Latest(); ok {
		t.Fatal("空缓存不应返回数据")
	}
	if got := r.History(0); got != nil {
		t.Fatalf("空缓存 History 应为 nil, got %v", got)
	}

	for i := int64(1); i <= 3; i++ {
		r.Push(sysSnap(i))
	}
	// 覆盖最旧一条
	r.Push(sysSnap(4))

	last, ok := r.Latest()
	if !ok || last.Timestamp != 4 {
		t.Fatalf("Latest 期望 4, got %d ok=%v", last.Timestamp, ok)
	}

	hist := r.History(0)
	want := []int64{2, 3, 4}
	if len(hist) != len(want) {
		t.Fatalf("History 长度期望 %d, got %d", len(want), len(hist))
	}
	for i, ts := range want {
		if hist[i].Timestamp != ts {
			t.Fatalf("History[%d] 期望 %d, got %d", i, ts, hist[i].Timestamp)
		}
	}
}

func TestRingHistoryLimit(t *testing.T) {
	r := newRing[model.SystemSnapshot](5)
	for i := int64(1); i <= 5; i++ {
		r.Push(sysSnap(i))
	}
	hist := r.History(2)
	if len(hist) != 2 || hist[0].Timestamp != 4 || hist[1].Timestamp != 5 {
		t.Fatalf("History(2) 期望 [4 5], got %v", []int64{hist[0].Timestamp, hist[1].Timestamp})
	}
}

func TestStoreSystemDocker(t *testing.T) {
	s := New(2)
	s.PushSystem(sysSnap(1))
	s.PushDocker(model.DockerListResult{Available: true, Message: "m"})

	if snap, ok := s.LatestSystem(); !ok || snap.Timestamp != 1 {
		t.Fatalf("Store.LatestSystem 异常: %v %v", snap, ok)
	}
	if res, ok := s.LatestDocker(); !ok || !res.Available || res.Message != "m" {
		t.Fatalf("Store.LatestDocker 异常: %+v %v", res, ok)
	}
}

func TestStoreNewSizeNonPositive(t *testing.T) {
	s := New(0) // 应退化为 size=1 而不 panic
	s.PushSystem(sysSnap(9))
	if snap, ok := s.LatestSystem(); !ok || snap.Timestamp != 9 {
		t.Fatalf("size<=0 时应仍可读写, got %v %v", snap, ok)
	}
}
