package handlers

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// liveDamageLogInterval throttles the running damage line so a feed that drops
// packets constantly logs a tally every half minute instead of once per packet.
const liveDamageLogInterval = 30 * time.Second

var (
	liveCorruptPacketPattern   = regexp.MustCompile(`Packet corrupt \(stream = (\d+)`)
	liveSkippedSegmentsPattern = regexp.MustCompile(`skipping (\d+) segments? ahead`)
)

// liveUpstreamDamage counts what FFmpeg reports losing from a live upstream:
// packets dropped by +discardcorrupt (per stream index) and HLS segments that
// expired before FFmpeg read them. A dropped video packet leaves a timestamp
// gap, which a player shows as frozen video while the audio carries on.
type liveUpstreamDamage struct {
	mu              sync.Mutex
	corruptByStream map[int]int
	skippedSegments int
	lastLogged      time.Time
	loggedTotal     int
}

func newLiveUpstreamDamage() *liveUpstreamDamage {
	return &liveUpstreamDamage{corruptByStream: map[int]int{}}
}

// observe tallies one chunk of FFmpeg stderr and, at most every
// liveDamageLogInterval, logs the running totals when they have grown.
func (d *liveUpstreamDamage) observe(sessionID, stderr string, now time.Time) {
	if d == nil {
		return
	}
	corrupt := liveCorruptPacketPattern.FindAllStringSubmatch(stderr, -1)
	skipped := liveSkippedSegmentsPattern.FindAllStringSubmatch(stderr, -1)
	if len(corrupt) == 0 && len(skipped) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, match := range corrupt {
		if stream, err := strconv.Atoi(match[1]); err == nil {
			d.corruptByStream[stream]++
		}
	}
	for _, match := range skipped {
		if count, err := strconv.Atoi(match[1]); err == nil {
			d.skippedSegments += count
		}
	}
	if total := d.totalLocked(); total > d.loggedTotal && now.Sub(d.lastLogged) >= liveDamageLogInterval {
		d.lastLogged = now
		d.loggedTotal = total
		log.Printf("[hls] live session %s: upstream damage so far %s", sessionID, d.summaryLocked())
	}
}

func (d *liveUpstreamDamage) totalLocked() int {
	total := d.skippedSegments
	for _, count := range d.corruptByStream {
		total += count
	}
	return total
}

func (d *liveUpstreamDamage) summaryLocked() string {
	streams := make([]int, 0, len(d.corruptByStream))
	for stream := range d.corruptByStream {
		streams = append(streams, stream)
	}
	sort.Ints(streams)
	parts := make([]string, 0, len(streams))
	for _, stream := range streams {
		parts = append(parts, fmt.Sprintf("%d:%d", stream, d.corruptByStream[stream]))
	}
	corrupt := "none"
	if len(parts) > 0 {
		corrupt = strings.Join(parts, ",")
	}
	return fmt.Sprintf("corrupt_dropped_by_stream=%s skipped_segments=%d", corrupt, d.skippedSegments)
}

// summary is the session-end tally; empty for sessions that never saw damage.
func (d *liveUpstreamDamage) summary() string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.totalLocked() == 0 {
		return ""
	}
	return d.summaryLocked()
}
