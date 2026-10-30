package handlers

import (
	"testing"
	"time"
)

func TestLiveUpstreamDamageTalliesFFmpegLosses(t *testing.T) {
	damage := newLiveUpstreamDamage()
	if got := damage.summary(); got != "" {
		t.Fatalf("summary before damage = %q, want empty", got)
	}

	now := time.Now()
	damage.observe("s1", "[h264 @ 0x1] Increasing reorder buffer to 2\n", now)
	damage.observe("s1", "[mpegts @ 0x2] Packet corrupt (stream = 0, dts = 484858), dropping it.\n"+
		"[mpegts @ 0x2] Packet corrupt (stream = 0, dts = 486360), dropping it.\n", now)
	damage.observe("s1", "[hls @ 0x3] skipping 2 segments ahead, expired from playlists\n"+
		"[mpegts @ 0x2] Packet corrupt (stream = 1, dts = 9), dropping it.\n", now)

	want := "corrupt_dropped_by_stream=0:2,1:1 skipped_segments=2"
	if got := damage.summary(); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}

	var nilDamage *liveUpstreamDamage
	nilDamage.observe("s1", "Packet corrupt (stream = 0", now)
	if got := nilDamage.summary(); got != "" {
		t.Fatalf("nil summary = %q, want empty", got)
	}
}
