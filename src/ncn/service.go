package ncn

import "strings"

// Service identifies an indication from the NCN5121 to the host controller.
type Service uint8

const (
	None Service = iota
	L_DataCon
	U_ResetInd
	U_StateInd
	U_FrameStateInd
	U_SystemStatInd
)

const (
	lDataStandardMask = 0xd3
	lDataStandard     = 0x90
	lDataConMask      = 0x7f
	lDataCon          = 0x0b
	lDataConPositive  = 0x80
	uResetInd         = 0x03
	uStateMask        = 0x07
	uState            = 0x07
	uFrameStateMask   = 0x17
	uFrameState       = 0x13
	uSystemStat       = 0x4b

	npciIndex     = 5
	lengthMask    = 0x0f
	frameOverhead = 8
)

// Parser splits the byte stream from the NCN5121 into KNX frames and service
// indications. Bytes belonging to a standard frame are skipped, so data
// octets are not mistaken for services. Extended frames are not recognised.
type Parser struct {
	n         int // octets of the current frame seen so far
	length    int // total frame length, 0 until the NPCI octet is seen
	systemHdr bool
}

// Feed processes one received byte. It returns the service it completes,
// together with its status byte, or None.
func (p *Parser) Feed(b byte) (Service, byte) {
	switch {
	case p.systemHdr:
		p.systemHdr = false
		return U_SystemStatInd, b
	case p.n > 0:
		p.n++
		if p.n == npciIndex+1 {
			p.length = frameOverhead + int(b&lengthMask)
		}
		if p.length > 0 && p.n == p.length {
			p.n, p.length = 0, 0
		}
		return None, 0
	case b&lDataStandardMask == lDataStandard:
		p.n = 1
		return None, 0
	case b == uSystemStat:
		p.systemHdr = true
		return None, 0
	case b == uResetInd:
		return U_ResetInd, b
	case b&lDataConMask == lDataCon:
		return L_DataCon, b
	case b&uStateMask == uState:
		return U_StateInd, b
	case b&uFrameStateMask == uFrameState:
		return U_FrameStateInd, b
	}
	return None, 0
}

// Describe renders a service returned by Parser.Feed.
func Describe(s Service, b byte) string {
	switch s {
	case L_DataCon:
		if b&lDataConPositive != 0 {
			return "L_Data.con: positive"
		}
		return "L_Data.con: negative"
	case U_ResetInd:
		return "U_Reset.ind"
	case U_StateInd:
		return "U_State.ind: " + DecodeState(b)
	case U_FrameStateInd:
		return "U_FrameState.ind: " + DecodeFrameState(b)
	case U_SystemStatInd:
		return "U_SystemStat.ind: " + DecodeSystemStat(b)
	}
	return ""
}

type flag struct {
	mask uint8
	text string
}

func decodeFlags(b byte, flags []flag, none string) string {
	var set [8]string
	n := 0
	for _, f := range flags {
		if b&f.mask != 0 {
			set[n] = f.text
			n++
		}
	}
	if n == 0 {
		return none
	}
	return strings.Join(set[:n], ", ")
}

var stateFlags = [...]flag{
	{0x80, "slave collision"},
	{0x40, "receive error (corrupted bytes from host)"},
	{0x20, "transmit error (sent 0, read 1)"},
	{0x10, "protocol error (invalid command sequence from host)"},
	{0x08, "thermal warning"},
}

// DecodeState describes the flags of a U_State.ind byte.
func DecodeState(b byte) string {
	return decodeFlags(b, stateFlags[:], "no errors")
}

var frameStateFlags = [...]flag{
	{0x80, "parity or bit error"},
	{0x40, "checksum or length error"},
	{0x20, "timing error"},
}

// DecodeFrameState describes the flags of a U_FrameState.ind byte.
func DecodeFrameState(b byte) string {
	return decodeFlags(b, frameStateFlags[:], "ok")
}

var systemStatBits = [...]flag{
	{0x80, "V20V"},
	{0x40, "VDD2"},
	{0x20, "VBUS"},
	{0x10, "VFILT"},
	{0x08, "XTAL"},
}

var modes = [...]string{"Power-Up", "Sync", "Stop", "Normal"}

// DecodeSystemStat describes the second byte of a U_SystemStat.ind.
func DecodeSystemStat(b byte) string {
	var parts [len(systemStatBits) + 2]string
	for i, f := range systemStatBits {
		if b&f.mask != 0 {
			parts[i] = f.text + " ok"
		} else {
			parts[i] = f.text + " out of range"
		}
	}
	parts[len(systemStatBits)] = "no thermal warning"
	if b&0x04 != 0 {
		parts[len(systemStatBits)] = "thermal warning"
	}
	parts[len(systemStatBits)+1] = "mode " + modes[b&0x03]
	return strings.Join(parts[:], ", ")
}
