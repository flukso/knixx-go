package ncn

// Service identifies an indication from the NCN5121 to the host controller.
type Service uint8

const (
	None Service = iota
	// L_DataInd is a standard frame from the bus, see Parser.Frame. The
	// transceiver also echoes the frames it sends.
	L_DataInd
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
	maxStdFrame   = frameOverhead + lengthMask
)

// Parser splits the byte stream from the NCN5121 into KNX frames and service
// indications. Bytes belonging to a standard frame are collected apart, so
// data octets are not mistaken for services. Extended frames are not
// recognised.
type Parser struct {
	n         int // octets of the current frame seen so far
	length    int // total frame length, 0 until the NPCI octet is seen
	systemHdr bool
	frame     [maxStdFrame]byte
	done      int // length of the last completed frame
}

// Frame returns the frame of the last L_DataInd, checksum included. It is
// valid until the next call of Feed.
func (p *Parser) Frame() []byte {
	return p.frame[:p.done]
}

// Feed processes one received byte. It returns the service it completes,
// together with its status byte, or None.
func (p *Parser) Feed(b byte) (Service, byte) {
	switch {
	case p.systemHdr:
		p.systemHdr = false
		return U_SystemStatInd, b
	case p.n > 0:
		p.frame[p.n] = b
		p.n++
		if p.n == npciIndex+1 {
			p.length = frameOverhead + int(b&lengthMask)
		}
		if p.length > 0 && p.n == p.length {
			p.done = p.length
			p.n, p.length = 0, 0
			return L_DataInd, 0
		}
		return None, 0
	case b&lDataStandardMask == lDataStandard:
		p.frame[0] = b
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

// ConPositive reports whether an L_Data.con status byte confirms delivery.
func ConPositive(b byte) bool {
	return b&lDataConPositive != 0
}

// Describe renders a service returned by Parser.Feed.
func Describe(s Service, b byte) string {
	return string(AppendDescribe(nil, s, b))
}

// AppendDescribe appends the rendering of a service returned by Parser.Feed
// to dst. It does not allocate when dst has room.
func AppendDescribe(dst []byte, s Service, b byte) []byte {
	switch s {
	case L_DataInd:
		return append(dst, "L_Data.ind"...)
	case L_DataCon:
		if ConPositive(b) {
			return append(dst, "L_Data.con: positive"...)
		}
		return append(dst, "L_Data.con: negative"...)
	case U_ResetInd:
		return append(dst, "U_Reset.ind"...)
	case U_StateInd:
		return appendFlags(append(dst, "U_State.ind: "...), b, stateFlags[:], "no errors")
	case U_FrameStateInd:
		return appendFlags(append(dst, "U_FrameState.ind: "...), b, frameStateFlags[:], "ok")
	case U_SystemStatInd:
		return AppendSystemStat(append(dst, "U_SystemStat.ind: "...), b)
	}
	return dst
}

type flag struct {
	mask uint8
	text string
}

// appendFlags appends the texts of the flags set in b, separated by commas,
// or none when no flag is set.
func appendFlags(dst []byte, b byte, flags []flag, none string) []byte {
	start := len(dst)
	for _, f := range flags {
		if b&f.mask == 0 {
			continue
		}
		if len(dst) > start {
			dst = append(dst, ", "...)
		}
		dst = append(dst, f.text...)
	}
	if len(dst) == start {
		dst = append(dst, none...)
	}
	return dst
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
	return string(appendFlags(nil, b, stateFlags[:], "no errors"))
}

var frameStateFlags = [...]flag{
	{0x80, "parity or bit error"},
	{0x40, "checksum or length error"},
	{0x20, "timing error"},
}

// DecodeFrameState describes the flags of a U_FrameState.ind byte.
func DecodeFrameState(b byte) string {
	return string(appendFlags(nil, b, frameStateFlags[:], "ok"))
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
	return string(AppendSystemStat(nil, b))
}

// AppendSystemStat appends the description of the second byte of a
// U_SystemStat.ind to dst.
func AppendSystemStat(dst []byte, b byte) []byte {
	for _, f := range systemStatBits {
		dst = append(dst, f.text...)
		if b&f.mask != 0 {
			dst = append(dst, " ok, "...)
		} else {
			dst = append(dst, " out of range, "...)
		}
	}
	if b&0x04 != 0 {
		dst = append(dst, "thermal warning"...)
	} else {
		dst = append(dst, "no thermal warning"...)
	}
	dst = append(dst, ", mode "...)
	return append(dst, modes[b&0x03]...)
}
