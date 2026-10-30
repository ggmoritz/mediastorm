package handlers

import (
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// tsInspector passively reads a tuner's MPEG-TS on its way to the player and logs
// what the player will be given: the PMT layout (codecs, languages, AC-4 on ATSC
// 3.0 channels), PMT changes, audio that stops while video continues, and
// continuity/transport errors from weak reception. It never alters the bytes.
type tsInspector struct {
	mu     sync.Mutex
	label  string
	now    func() time.Time
	logf   func(format string, args ...any)
	window time.Duration

	carry       []byte
	pmtPID      int
	pmtVersion  int
	streams     map[int]tsElementaryStream
	lastCC      map[int]byte
	windowStart time.Time
	windowPkts  map[int]int
	quietAudio  map[int]bool
	started     time.Time
	packets     int64
	ccErrors    int64
	teiPackets  int64
	pmtChanges  int
	audioGaps   int
	windowCC    int64
	windowTEI   int64
	summaryOnce sync.Once
}

type tsElementaryStream struct {
	PID        int
	StreamType byte
	Kind       string
	Codec      string
	Language   string
}

const tsPacketSize = 188

func newTSInspector(label string) *tsInspector {
	return &tsInspector{
		label:      label,
		now:        time.Now,
		logf:       log.Printf,
		window:     5 * time.Second,
		pmtPID:     -1,
		pmtVersion: -1,
		streams:    map[int]tsElementaryStream{},
		lastCC:     map[int]byte{},
		windowPkts: map[int]int{},
		quietAudio: map[int]bool{},
	}
}

func (t *tsInspector) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if t.started.IsZero() {
		t.started, t.windowStart = now, now
	}
	data := p
	if len(t.carry) > 0 {
		data = append(t.carry, p...)
		t.carry = nil
	}
	for len(data) >= tsPacketSize {
		if data[0] != 0x47 {
			// Resynchronise on the next sync byte; tuners send aligned packets,
			// so this only happens on a damaged or truncated read.
			next := 1
			for next < len(data) && data[next] != 0x47 {
				next++
			}
			data = data[next:]
			continue
		}
		t.packet(data[:tsPacketSize])
		data = data[tsPacketSize:]
	}
	if len(data) > 0 {
		t.carry = append([]byte(nil), data...)
	}
	if now.Sub(t.windowStart) >= t.window {
		t.closeWindow(now)
	}
	return len(p), nil
}

func (t *tsInspector) packet(pkt []byte) {
	t.packets++
	if pkt[1]&0x80 != 0 {
		t.teiPackets++
		t.windowTEI++
		return
	}
	pid := int(pkt[1]&0x1f)<<8 | int(pkt[2])
	if pid == 0x1fff {
		return
	}
	payloadStart := pkt[1]&0x40 != 0
	adaptation := (pkt[3] >> 4) & 0x3
	cc := pkt[3] & 0x0f
	hasPayload := adaptation&0x1 != 0
	if hasPayload {
		discontinuity := adaptation&0x2 != 0 && pkt[4] > 0 && pkt[5]&0x80 != 0
		if last, seen := t.lastCC[pid]; seen && !discontinuity && cc != (last+1)&0x0f && cc != last {
			t.ccErrors++
			t.windowCC++
		}
		t.lastCC[pid] = cc
	}
	t.windowPkts[pid]++
	if !hasPayload || !payloadStart || (pid != 0 && pid != t.pmtPID) {
		return
	}
	payload := pkt[4:]
	if adaptation&0x2 != 0 {
		if int(pkt[4])+1 >= len(payload) {
			return
		}
		payload = payload[int(pkt[4])+1:]
	}
	if len(payload) < 1 || int(payload[0])+1 >= len(payload) {
		return
	}
	section := payload[int(payload[0])+1:]
	if pid == 0 {
		t.parsePAT(section)
	} else {
		t.parsePMT(section)
	}
}

func tsSectionBody(section []byte, tableID byte) ([]byte, byte, bool) {
	if len(section) < 8 || section[0] != tableID {
		return nil, 0, false
	}
	length := int(section[1]&0x0f)<<8 | int(section[2])
	// Single-packet sections only; HDHomeRun PAT/PMT always fit in one packet.
	if length < 9 || 3+length > len(section) {
		return nil, 0, false
	}
	version := (section[5] >> 1) & 0x1f
	return section[8 : 3+length-4], version, true
}

func (t *tsInspector) parsePAT(section []byte) {
	body, _, ok := tsSectionBody(section, 0x00)
	if !ok {
		return
	}
	for i := 0; i+4 <= len(body); i += 4 {
		program := int(body[i])<<8 | int(body[i+1])
		if program != 0 {
			t.pmtPID = int(body[i+2]&0x1f)<<8 | int(body[i+3])
			return
		}
	}
}

func (t *tsInspector) parsePMT(section []byte) {
	body, version, ok := tsSectionBody(section, 0x02)
	if !ok || int(version) == t.pmtVersion || len(body) < 4 {
		return
	}
	infoLength := int(body[2]&0x0f)<<8 | int(body[3])
	if 4+infoLength > len(body) {
		return
	}
	streams := map[int]tsElementaryStream{}
	for i := 4 + infoLength; i+5 <= len(body); {
		esLength := int(body[i+3]&0x0f)<<8 | int(body[i+4])
		if i+5+esLength > len(body) {
			break
		}
		stream := tsElementaryStream{PID: int(body[i+1]&0x1f)<<8 | int(body[i+2]), StreamType: body[i]}
		stream.Kind, stream.Codec = tsStreamCodec(body[i], body[i+5:i+5+esLength])
		stream.Language = tsDescriptorLanguage(body[i+5 : i+5+esLength])
		streams[stream.PID] = stream
		i += 5 + esLength
	}
	previous := t.pmtVersion
	t.pmtVersion = int(version)
	t.streams = streams
	if previous >= 0 {
		t.pmtChanges++
		t.logf("[hdhomerun-ts] %s PMT changed version=%d->%d after=%s streams=%s", t.label, previous, version, t.elapsed(), t.describeStreams())
		return
	}
	t.logf("[hdhomerun-ts] %s layout after=%s streams=%s", t.label, t.elapsed(), t.describeStreams())
}

// tsStreamCodec maps ATSC/DVB stream types and registration descriptors to names.
func tsStreamCodec(streamType byte, descriptors []byte) (string, string) {
	switch streamType {
	case 0x01, 0x02:
		return "video", "mpeg2"
	case 0x1b:
		return "video", "h264"
	case 0x24:
		return "video", "hevc"
	case 0x03, 0x04:
		return "audio", "mp2"
	case 0x0f:
		return "audio", "aac"
	case 0x11:
		return "audio", "aac-latm"
	case 0x81:
		return "audio", "ac3"
	case 0x87:
		return "audio", "eac3"
	case 0x86:
		return "data", "scte35"
	}
	for i := 0; i+2 <= len(descriptors); {
		tag, length := descriptors[i], int(descriptors[i+1])
		if i+2+length > len(descriptors) {
			break
		}
		value := descriptors[i+2 : i+2+length]
		switch {
		case tag == 0x6a:
			return "audio", "ac3"
		case tag == 0x7a:
			return "audio", "eac3"
		case tag == 0x7f && length > 0 && value[0] == 0x15:
			return "audio", "ac4"
		case tag == 0x05 && length >= 4 && string(value[:4]) == "AC-4":
			return "audio", "ac4"
		case tag == 0x05 && length >= 4 && string(value[:4]) == "AC-3":
			return "audio", "ac3"
		}
		i += 2 + length
	}
	return "other", fmt.Sprintf("type0x%02x", streamType)
}

func tsDescriptorLanguage(descriptors []byte) string {
	for i := 0; i+2 <= len(descriptors); {
		tag, length := descriptors[i], int(descriptors[i+1])
		if i+2+length > len(descriptors) {
			break
		}
		if tag == 0x0a && length >= 3 {
			return string(descriptors[i+2 : i+5])
		}
		i += 2 + length
	}
	return ""
}

func (t *tsInspector) describeStreams() string {
	pids := make([]int, 0, len(t.streams))
	for pid := range t.streams {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		s := t.streams[pid]
		part := fmt.Sprintf("%s:%s(pid=%d", s.Kind, s.Codec, s.PID)
		if s.Language != "" {
			part += ",lang=" + s.Language
		}
		parts = append(parts, part+")")
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func (t *tsInspector) closeWindow(now time.Time) {
	videoPackets := 0
	for pid, stream := range t.streams {
		if stream.Kind == "video" {
			videoPackets += t.windowPkts[pid]
		}
	}
	for pid, stream := range t.streams {
		if stream.Kind != "audio" {
			continue
		}
		quiet := t.windowPkts[pid] == 0 && videoPackets > 0
		if quiet && !t.quietAudio[pid] {
			t.audioGaps++
			t.logf("[hdhomerun-ts] %s audio stopped while video continues pid=%d codec=%s lang=%q after=%s videoPackets=%d",
				t.label, pid, stream.Codec, stream.Language, t.elapsed(), videoPackets)
		} else if !quiet && t.quietAudio[pid] {
			t.logf("[hdhomerun-ts] %s audio resumed pid=%d after=%s", t.label, pid, t.elapsed())
		}
		t.quietAudio[pid] = quiet
	}
	if t.windowCC > 0 || t.windowTEI > 0 {
		t.logf("[hdhomerun-ts] %s stream damage window=%s continuityErrors=%d transportErrors=%d after=%s",
			t.label, t.window, t.windowCC, t.windowTEI, t.elapsed())
	}
	t.windowCC, t.windowTEI = 0, 0
	t.windowPkts = map[int]int{}
	t.windowStart = now
}

func (t *tsInspector) elapsed() time.Duration {
	if t.started.IsZero() {
		return 0
	}
	return t.now().Sub(t.started).Round(100 * time.Millisecond)
}

func (t *tsInspector) Summary() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.summaryOnce.Do(func() {
		if t.packets == 0 {
			return
		}
		layout := "none"
		if t.pmtVersion >= 0 {
			layout = t.describeStreams()
		}
		t.logf("[hdhomerun-ts] %s end after=%s packets=%d continuityErrors=%d transportErrors=%d pmtChanges=%d audioGaps=%d streams=%s",
			t.label, t.elapsed(), t.packets, t.ccErrors, t.teiPackets, t.pmtChanges, t.audioGaps, layout)
	})
}

// inspectedTSBody feeds every byte read from the tuner to a tsInspector.
type inspectedTSBody struct {
	io.ReadCloser
	inspector *tsInspector
}

func newInspectedTSBody(body io.ReadCloser, label string) *inspectedTSBody {
	return &inspectedTSBody{ReadCloser: body, inspector: newTSInspector(label)}
}

func (b *inspectedTSBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, _ = b.inspector.Write(p[:n])
	}
	if err != nil {
		b.inspector.Summary()
	}
	return n, err
}

func (b *inspectedTSBody) Close() error {
	b.inspector.Summary()
	return b.ReadCloser.Close()
}
