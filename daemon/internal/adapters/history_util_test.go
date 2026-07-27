package adapters

import "testing"

func TestClampHistoryLimit(t *testing.T) {
	if clampHistoryLimit(0) != 50 || clampHistoryLimit(-1) != 50 {
		t.Fatal("default")
	}
	if clampHistoryLimit(100) != 40 {
		t.Fatal("cap")
	}
	if clampHistoryLimit(10) != 10 {
		t.Fatal("ok")
	}
}

func TestTruncateRunes(t *testing.T) {
	if truncateRunes("hello", 10) != "hello" {
		t.Fatal("short")
	}
	if truncateRunes("hello", 3) != "hel…" {
		t.Fatalf("%q", truncateRunes("hello", 3))
	}
	if truncateRunes("你好世界", 2) != "你好…" {
		t.Fatalf("cjk %q", truncateRunes("你好世界", 2))
	}
	if truncateRunes("x", 0) != "x" {
		t.Fatal("max0")
	}
	if truncateRunes("", 5) != "" {
		t.Fatal("empty")
	}
}

func TestTakeLastHistory(t *testing.T) {
	msgs := make([]*HistoryMessage, 0, 60)
	for i := 0; i < 60; i++ {
		msgs = append(msgs, &HistoryMessage{ID: string(rune('a' + i%26))})
	}
	out := takeLastHistory(msgs, 10)
	if len(out) != 10 {
		t.Fatalf("len %d", len(out))
	}
	if out[0] != msgs[50] || out[9] != msgs[59] {
		t.Fatal("tail")
	}
	if takeLastHistory(msgs[:3], 10)[0] != msgs[0] {
		t.Fatal("under")
	}
}
