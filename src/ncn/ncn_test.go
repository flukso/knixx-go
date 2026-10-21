package ncn

import (
	"bytes"
	"testing"
)

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestTxData(t *testing.T) {
	var w countingWriter
	frame := []byte{0xbc, 0x78, 0x01, 0x00, 0x02, 0x81, 0x00, 0x80, 0x39}
	n, err := TxData(&w, frame, new(TxBuf))
	if err != nil || n != len(frame) {
		t.Fatalf("TxData = %d, %v", n, err)
	}
	want := []byte{
		0x80, 0xbc, 0x81, 0x78, 0x82, 0x01, 0x83, 0x00, 0x84, 0x02,
		0x85, 0x81, 0x86, 0x00, 0x87, 0x80, 0x48, 0x39,
	}
	if !bytes.Equal(w.Bytes(), want) {
		t.Errorf("got % x, want % x", w.Bytes(), want)
	}
	if w.writes != 1 {
		t.Errorf("%d writes, want 1", w.writes)
	}
	if _, err := TxData(&w, make([]byte, maxFrameLen+1), new(TxBuf)); err == nil {
		t.Error("oversized frame: expected an error")
	}
}

type event struct {
	s Service
	b byte
}

func feed(data []byte) []event {
	var p Parser
	var events []event
	for _, b := range data {
		if s, v := p.Feed(b); s != None {
			events = append(events, event{s, v})
		}
	}
	return events
}

func TestParser(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []event
	}{
		{"clean echo",
			[]byte{0xbc, 0x78, 0x01, 0x00, 0x02, 0x81, 0x00, 0x80, 0x39, 0x13, 0x8b},
			[]event{{L_DataInd, 0}, {U_FrameStateInd, 0x13}, {L_DataCon, 0x8b}}},
		{"data octets that look like services",
			[]byte{0xbc, 0x07, 0x03, 0x4b, 0x13, 0x81, 0x00, 0x8b, 0x17, 0x0b},
			[]event{{L_DataInd, 0}, {L_DataCon, 0x0b}}},
		{"protocol and receive errors, reset",
			[]byte{0x17, 0x47, 0x03},
			[]event{{U_StateInd, 0x17}, {U_StateInd, 0x47}, {U_ResetInd, 0x03}}},
		{"stat reply",
			[]byte{0x07, 0x4b, 0xfb},
			[]event{{U_StateInd, 0x07}, {U_SystemStatInd, 0xfb}}},
	}
	for _, tt := range tests {
		got := feed(tt.data)
		if len(got) != len(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

func TestParserFrame(t *testing.T) {
	short := []byte{0xbc, 0x78, 0x01, 0x00, 0x02, 0x81, 0x00, 0x80, 0x39}
	long := []byte{0xbc, 0x11, 0x05, 0x00, 0x03, 0xef,
		0x00, 0x80, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 0x00}
	var p Parser
	if f := p.Frame(); len(f) != 0 {
		t.Errorf("Frame before any frame = % x", f)
	}
	for _, frame := range [][]byte{short, long, short} {
		for i, b := range frame {
			s, _ := p.Feed(b)
			if last := i == len(frame)-1; last != (s == L_DataInd) {
				t.Fatalf("octet %d of % x: service %d", i, frame, s)
			}
		}
		if !bytes.Equal(p.Frame(), frame) {
			t.Errorf("Frame = % x, want % x", p.Frame(), frame)
		}
		// A service after the frame leaves it in place.
		if s, _ := p.Feed(0x8b); s != L_DataCon || !bytes.Equal(p.Frame(), frame) {
			t.Errorf("after L_Data.con: service %d, Frame % x", s, p.Frame())
		}
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		s    Service
		b    byte
		want string
	}{
		{L_DataInd, 0, "L_Data.ind"},
		{L_DataCon, 0x8b, "L_Data.con: positive"},
		{L_DataCon, 0x0b, "L_Data.con: negative"},
		{U_ResetInd, 0x03, "U_Reset.ind"},
		{U_StateInd, 0x07, "U_State.ind: no errors"},
		{U_StateInd, 0x17, "U_State.ind: protocol error (invalid command sequence from host)"},
		{U_StateInd, 0x47, "U_State.ind: receive error (corrupted bytes from host)"},
		{U_StateInd, 0xff, "U_State.ind: slave collision, receive error (corrupted bytes from host), " +
			"transmit error (sent 0, read 1), protocol error (invalid command sequence from host), thermal warning"},
		{U_FrameStateInd, 0x13, "U_FrameState.ind: ok"},
		{U_FrameStateInd, 0xd3, "U_FrameState.ind: parity or bit error, checksum or length error"},
		{U_SystemStatInd, 0xfb, "U_SystemStat.ind: V20V ok, VDD2 ok, VBUS ok, VFILT ok, XTAL ok, " +
			"no thermal warning, mode Normal"},
		{U_SystemStatInd, 0x5d, "U_SystemStat.ind: V20V out of range, VDD2 ok, VBUS out of range, VFILT ok, XTAL ok, " +
			"thermal warning, mode Sync"},
	}
	for _, tt := range tests {
		if got := Describe(tt.s, tt.b); got != tt.want {
			t.Errorf("Describe(%d, %#02x) = %q, want %q", tt.s, tt.b, got, tt.want)
		}
	}
}

func TestAppendDescribeAllocs(t *testing.T) {
	var buf [256]byte
	allocs := testing.AllocsPerRun(100, func() {
		for _, s := range []Service{L_DataCon, U_StateInd, U_FrameStateInd, U_SystemStatInd} {
			AppendDescribe(buf[:0], s, 0xff)
		}
	})
	if allocs != 0 {
		t.Errorf("AppendDescribe allocated %v times", allocs)
	}
}
