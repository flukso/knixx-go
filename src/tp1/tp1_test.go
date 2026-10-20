package tp1

import (
	"bytes"
	"reflect"
	"testing"
)

// groupWrite is the telegram of a DPT 1.001 on from 7.8.1 to 0/0/1, and
// groupWriteFrame its frame.
var (
	groupWrite = Telegram{
		Ctrl: Ctrl{Repeat: false, Priority: PriorityLow},
		Src:  0x7801,
		Dest: 0x0001,
		Npci: Npci{AddrType: AddrGroup, Routing: 0, Length: 1},
		Tpci: Tpci{Transport: UnnumberedDataPacket, Sequence: 0},
		Apci: GroupValueWrite,
		Data: []byte{0x01},
	}
	groupWriteFrame = []byte{0xbc, 0x78, 0x01, 0x00, 0x01, 0x81, 0x00, 0x81, 0x3b}
)

func TestPackFrame(t *testing.T) {
	buf, err := groupWrite.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, groupWriteFrame) {
		t.Errorf("Pack = % x, want % x", buf, groupWriteFrame)
	}
}

func TestUnpackFrame(t *testing.T) {
	var tg Telegram
	if err := tg.Unpack(groupWriteFrame); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tg, groupWrite) {
		t.Errorf("Unpack = %+v, want %+v", tg, groupWrite)
	}
}

// with returns groupWrite changed by f.
func with(f func(*Telegram)) Telegram {
	tg := groupWrite
	tg.Data = append([]byte(nil), groupWrite.Data...)
	f(&tg)
	tg.Length = Length(len(tg.Data))
	return tg
}

func TestPackUnpack(t *testing.T) {
	tests := map[string]Telegram{
		"group write 1 bit": groupWrite,
		"group write off":   with(func(tg *Telegram) { tg.Data = []byte{0x00} }),
		"group write 6 bit": with(func(tg *Telegram) { tg.Data = []byte{0x3f} }),
		"group write 2 octets": with(func(tg *Telegram) {
			tg.Data = []byte{0x00, 0x0c, 0x1a} // DPT 9.001 21.0 °C
		}),
		"max payload": with(func(tg *Telegram) {
			tg.Data = []byte{0x00, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 0xff}
		}),
		"group read": with(func(tg *Telegram) {
			tg.Apci, tg.Data = GroupValueRead, []byte{0x00}
		}),
		"repeated": with(func(tg *Telegram) { tg.Repeat = true }),
		"individual destination, routing 6": with(func(tg *Telegram) {
			tg.AddrType, tg.Routing, tg.Dest = AddrIndividual, 6, 0x1105
		}),
		"numbered data, sequence 9": with(func(tg *Telegram) {
			tg.Transport, tg.Sequence = NumberedDataPacket, 9
		}),
		"extreme addresses": with(func(tg *Telegram) { tg.Src, tg.Dest = 0xffff, 0x0000 }),
	}
	for p := PrioritySystem; p <= PriorityLow; p++ {
		tests["priority "+string(rune('0'+p))] = with(func(tg *Telegram) { tg.Priority = p })
	}
	for a := GroupValueRead; a <= Escape; a++ {
		tests["apci "+string(rune('a'+a))] = with(func(tg *Telegram) {
			tg.Apci, tg.Data = a, []byte{0x2a, 0xc3}
		})
	}

	for name, want := range tests {
		buf, err := want.Pack()
		if err != nil {
			t.Errorf("%s: Pack: %v", name, err)
			continue
		}
		var got Telegram
		if err := got.Unpack(buf); err != nil {
			t.Errorf("%s: Unpack(% x): %v", name, buf, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Unpack(Pack()) = %+v, want %+v", name, got, want)
		}
		// Packing the result again gives the same frame.
		again, err := got.Pack()
		if err != nil || !bytes.Equal(again, buf) {
			t.Errorf("%s: Pack(Unpack()) = % x, %v, want % x", name, again, err, buf)
		}
	}
}

// TestUnpackNoData covers a frame without APCI octet: Pack drops the low
// APCI bits, which Unpack returns cleared.
func TestUnpackNoData(t *testing.T) {
	tg := with(func(tg *Telegram) { tg.Apci, tg.Data = MemoryRead, nil })
	buf, err := tg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if len(buf) != 8 {
		t.Fatalf("Pack = % x, want 8 octets", buf)
	}
	var got Telegram
	if err := got.Unpack(buf); err != nil {
		t.Fatal(err)
	}
	tg.Apci = MemoryRead &^ 0x03
	if !reflect.DeepEqual(got, tg) {
		t.Errorf("Unpack = %+v, want %+v", got, tg)
	}
}

func TestUnpackCopiesData(t *testing.T) {
	buf := append([]byte(nil), groupWriteFrame...)
	var tg Telegram
	if err := tg.Unpack(buf); err != nil {
		t.Fatal(err)
	}
	buf[apci] = 0x80
	if tg.Data[0] != 0x01 {
		t.Errorf("Data follows the frame buffer: %x", tg.Data)
	}
}

func TestUnpackErrors(t *testing.T) {
	// frame returns groupWriteFrame changed by f, with its checksum fixed.
	frame := func(f func([]byte) []byte) []byte {
		buf := f(append([]byte(nil), groupWriteFrame...))
		buf[len(buf)-1] = checksum(buf[:len(buf)-1])
		return buf
	}
	tests := map[string][]byte{
		"empty":     nil,
		"too short": groupWriteFrame[:7],
		"checksum": func() []byte {
			buf := append([]byte(nil), groupWriteFrame...)
			buf[len(buf)-1] ^= 0x01
			return buf
		}(),
		"extended frame": frame(func(b []byte) []byte { b[ctrl] &^= 0x80; return b }),
		"poll frame":     frame(func(b []byte) []byte { b[ctrl] |= 0x40; return b }),
		"length too long": frame(func(b []byte) []byte {
			b[npci] = b[npci]&^0x0f | 2
			return b
		}),
		"length too short": frame(func(b []byte) []byte {
			b[npci] &^= 0x0f
			return b
		}),
		"control transport": frame(func(b []byte) []byte {
			b[tpci] = byte(UnnumberedControlData) << transportOffset
			return b
		}),
	}
	for name, buf := range tests {
		tg := groupWrite
		if err := tg.Unpack(buf); err == nil {
			t.Errorf("%s: Unpack(% x) succeeded: %+v", name, buf, tg)
		} else if !reflect.DeepEqual(tg, groupWrite) {
			t.Errorf("%s: Unpack(% x) changed the telegram to %+v", name, buf, tg)
		}
	}
}

func TestNames(t *testing.T) {
	for _, tt := range []struct {
		got, want string
	}{
		{PrioritySystem.String(), "system"},
		{PriorityLow.String(), "low"},
		{Priority(7).String(), "priority 7"},
		{GroupValueRead.String(), "GroupValueRead"},
		{GroupValueWrite.String(), "GroupValueWrite"},
		{Escape.String(), "Escape"},
		{Apci(16).String(), "APCI 16"},
	} {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
