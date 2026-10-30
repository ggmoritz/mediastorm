package handlers

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func tsTestPacket(pid int, payloadStart bool, cc byte, payload []byte) []byte {
	pkt := make([]byte, tsPacketSize)
	for i := range pkt {
		pkt[i] = 0xff
	}
	pkt[0] = 0x47
	pkt[1] = byte(pid>>8) & 0x1f
	if payloadStart {
		pkt[1] |= 0x40
	}
	pkt[2] = byte(pid)
	pkt[3] = 0x10 | cc&0x0f
	copy(pkt[4:], payload)
	return pkt
}

// tsTestSection wraps a table body with its header and a placeholder CRC; the
// inspector does not validate CRCs.
func tsTestSection(tableID byte, version byte, body []byte) []byte {
	length := 5 + len(body) + 4
	section := []byte{tableID, 0xb0 | byte(length>>8), byte(length), 0x00, 0x01, 0xc1 | version<<1, 0x00, 0x00}
	section = append(section, body...)
	return append(section, 0, 0, 0, 0)
}

func tsTestPAT(pmtPID int) []byte {
	return tsTestSection(0x00, 0, []byte{0x00, 0x01, 0xe0 | byte(pmtPID>>8), byte(pmtPID)})
}

type tsTestES struct {
	streamType  byte
	pid         int
	descriptors []byte
}

func tsTestPMT(version byte, streams ...tsTestES) []byte {
	body := []byte{0xe0, 0x31, 0xf0, 0x00}
	for _, s := range streams {
		body = append(body, s.streamType, 0xe0|byte(s.pid>>8), byte(s.pid), 0xf0|byte(len(s.descriptors)>>8), byte(len(s.descriptors)))
		body = append(body, s.descriptors...)
	}
	return tsTestSection(0x02, version, body)
}

type tsTestHarness struct {
	inspector *tsInspector
	clock     time.Time
	logs      []string
	cc        map[int]byte
}

func newTSTestHarness() *tsTestHarness {
	h := &tsTestHarness{clock: time.Unix(1_800_000_000, 0), cc: map[int]byte{}}
	h.inspector = newTSInspector("test")
	h.inspector.now = func() time.Time { return h.clock }
	h.inspector.logf = func(format string, args ...any) { h.logs = append(h.logs, fmt.Sprintf(format, args...)) }
	return h
}

func (h *tsTestHarness) send(pid int, payloadStart bool, payload []byte) {
	_, _ = h.inspector.Write(tsTestPacket(pid, payloadStart, h.cc[pid], payload))
	h.cc[pid] = (h.cc[pid] + 1) & 0x0f
}

func (h *tsTestHarness) logged(fragment string) bool {
	for _, line := range h.logs {
		if strings.Contains(line, fragment) {
			return true
		}
	}
	return false
}

var (
	tsTestVideo = tsTestES{streamType: 0x02, pid: 0x31}
	tsTestAC3   = tsTestES{streamType: 0x81, pid: 0x34, descriptors: []byte{0x0a, 0x04, 'e', 'n', 'g', 0x00}}
	tsTestAC4   = tsTestES{streamType: 0x06, pid: 0x35, descriptors: []byte{0x7f, 0x02, 0x15, 0x00, 0x0a, 0x04, 's', 'p', 'a', 0x00}}
)

func TestTSInspectorLogsLayoutIncludingAC4(t *testing.T) {
	h := newTSTestHarness()
	h.send(0, true, append([]byte{0}, tsTestPAT(0x30)...))
	h.send(0x30, true, append([]byte{0}, tsTestPMT(0, tsTestVideo, tsTestAC3, tsTestAC4)...))
	want := "streams=[video:mpeg2(pid=49) audio:ac3(pid=52,lang=eng) audio:ac4(pid=53,lang=spa)]"
	if !h.logged("layout") || !h.logged(want) {
		t.Fatalf("layout not logged as %q: %v", want, h.logs)
	}
}

func TestTSInspectorReportsAudioGapPMTChangeAndDamage(t *testing.T) {
	h := newTSTestHarness()
	h.send(0, true, append([]byte{0}, tsTestPAT(0x30)...))
	h.send(0x30, true, append([]byte{0}, tsTestPMT(0, tsTestVideo, tsTestAC3)...))
	for i := 0; i < 10; i++ {
		h.send(0x31, false, nil)
		h.send(0x34, false, nil)
	}
	h.clock = h.clock.Add(6 * time.Second)
	h.send(0x31, false, nil) // closes the first window with audio present
	if h.logged("audio stopped") {
		t.Fatalf("audio reported missing while present: %v", h.logs)
	}

	for i := 0; i < 10; i++ {
		h.send(0x31, false, nil)
	}
	h.cc[0x31] += 3 // dropped packets
	h.send(0x31, false, nil)
	h.clock = h.clock.Add(6 * time.Second)
	h.send(0x31, false, nil)
	if !h.logged("audio stopped while video continues pid=52 codec=ac3") {
		t.Fatalf("audio gap not logged: %v", h.logs)
	}
	if !h.logged("continuityErrors=1") {
		t.Fatalf("continuity error not logged: %v", h.logs)
	}

	h.send(0x30, true, append([]byte{0}, tsTestPMT(1, tsTestVideo, tsTestAC4)...))
	if !h.logged("PMT changed version=0->1") {
		t.Fatalf("PMT change not logged: %v", h.logs)
	}
	h.inspector.Summary()
	h.inspector.Summary()
	ends := 0
	for _, line := range h.logs {
		if strings.Contains(line, " end ") {
			ends++
			if !strings.Contains(line, "pmtChanges=1 audioGaps=1") {
				t.Fatalf("summary = %q", line)
			}
		}
	}
	if ends != 1 {
		t.Fatalf("summary logged %d times", ends)
	}
}

func TestTSInspectorHandlesPacketsSplitAcrossReads(t *testing.T) {
	h := newTSTestHarness()
	stream := append(tsTestPacket(0, true, 0, append([]byte{0}, tsTestPAT(0x30)...)),
		tsTestPacket(0x30, true, 0, append([]byte{0}, tsTestPMT(0, tsTestVideo, tsTestAC3)...))...)
	_, _ = h.inspector.Write(stream[:100])
	_, _ = h.inspector.Write(stream[100:250])
	_, _ = h.inspector.Write(stream[250:])
	if !h.logged("audio:ac3(pid=52,lang=eng)") {
		t.Fatalf("split packets not reassembled: %v", h.logs)
	}
}
