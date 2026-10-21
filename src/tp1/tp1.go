package tp1

import (
	"encoding/binary"
	"errors"
	"strconv"
)

const (
	ctrl = iota
	src1
	src2
	dest1
	dest2
	npci
	tpci
	apci
)

const (
	maxPayloadLength = 15
)

// MaxPayload is the longest Data of a standard frame, and MaxFrame the
// longest frame, checksum included.
const (
	MaxPayload = maxPayloadLength
	MaxFrame   = maxPayloadLength + 8
)

var (
	errPayload   = errors.New("tp1: max payload length exceeded")
	errTransport = errors.New("tp1: control data transports not yet supported")
	errShort     = errors.New("tp1: frame too short")
	errChecksum  = errors.New("tp1: checksum mismatch")
	errStandard  = errors.New("tp1: not a standard frame")
	errLength    = errors.New("tp1: length does not match the frame")
)

const (
	ctrlDefault     = 0x90
	priorityOffset  = 2
	repeatOffset    = 5
	addrTypeOffset  = 7
	routingOffset   = 4
	routingMask     = 0x07
	transportOffset = 6
	sequenceOffset  = 2
	sequenceMask    = 0x0f
	apci1NegOffset  = 2
	apci2Offset     = 6
)

type Priority uint8

const (
	PrioritySystem Priority = iota
	PriorityHigh
	PriorityAlarm
	PriorityLow
)

type Repeat bool

type Ctrl struct {
	Repeat
	Priority
}

type Address uint16
type AddrType uint8

const (
	AddrIndividual AddrType = iota
	AddrGroup
)

type Routing uint8
type Length uint8

type Npci struct {
	AddrType
	Routing
	Length
}

type Transport uint8

const (
	UnnumberedDataPacket Transport = iota
	NumberedDataPacket
	UnnumberedControlData
	NumberedControlData
)

type Sequence uint8

type Tpci struct {
	Transport
	Sequence
}

type Apci uint8

const (
	GroupValueRead Apci = iota
	GroupValueResponse
	GroupValueWrite
	IndividualAddrWrite
	IndividualAddrRequest
	IndividualAddrResponse
	AdcRead
	AdcResponse
	MemoryRead
	MemoryResponse
	MemoryWrite
	UserMessage
	MaskVersionRead
	MaskVersionResponse
	Restart
	Escape
)

type Telegram struct {
	Ctrl
	Src  uint16
	Dest uint16
	Npci
	Tpci
	Apci
	Data []byte
}

// Pack encodes tg as a standard frame, checksum included, in a new slice.
// The firmware uses PackTo, which does not allocate.
func (tg *Telegram) Pack() ([]byte, error) {
	var buf [MaxFrame]byte
	frame, err := tg.PackTo(&buf)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), frame...), nil
}

// PackTo encodes tg as a standard frame, checksum included, into dst and
// returns the part of dst it fills.
func (tg *Telegram) PackTo(dst *[MaxFrame]byte) ([]byte, error) {
	if len(tg.Data) > maxPayloadLength {
		return nil, errPayload
	}
	if tg.Transport > NumberedDataPacket {
		return nil, errTransport
	}
	buffer := dst[:len(tg.Data)+8]
	clear(buffer)
	buffer[ctrl] = ctrlDefault |
		(repeat2uint8(tg.Repeat) << repeatOffset) |
		(uint8(tg.Priority) << priorityOffset)
	binary.BigEndian.PutUint16(buffer[src1:dest1], tg.Src)
	binary.BigEndian.PutUint16(buffer[dest1:npci], tg.Dest)
	buffer[npci] = (uint8(tg.AddrType) << addrTypeOffset) |
		((uint8(tg.Routing) & routingMask) << routingOffset) |
		uint8(len(tg.Data))
	buffer[tpci] = (uint8(tg.Transport) << transportOffset) |
		((uint8(tg.Sequence) & sequenceMask) << sequenceOffset) |
		(uint8(tg.Apci) >> apci1NegOffset)
	copy(buffer[apci:], tg.Data)
	buffer[apci] |= (uint8(tg.Apci) << apci2Offset)
	buffer[len(buffer)-1] = checksum(buffer[:len(buffer)-1])

	return buffer, nil
}

// Unpack decodes a standard frame as built by Pack, checksum included, into
// tg. Data gets the octets from the APCI octet on, with the APCI bits of its
// first octet cleared; Length is set to its length. Data reuses its own
// capacity, so a telegram with room for MaxPayload octets is unpacked
// without allocating; it never shares memory with buffer.
func (tg *Telegram) Unpack(buffer []byte) error {
	if len(buffer) < apci+1 {
		return errShort
	}
	if checksum(buffer[:len(buffer)-1]) != buffer[len(buffer)-1] {
		return errChecksum
	}
	if buffer[ctrl]&^(1<<repeatOffset|3<<priorityOffset) != ctrlDefault&^(1<<repeatOffset) {
		return errStandard
	}
	length := int(buffer[npci] & maxPayloadLength)
	if len(buffer) != length+8 {
		return errLength
	}
	transport := Transport(buffer[tpci] >> transportOffset)
	if transport > NumberedDataPacket {
		return errTransport
	}

	tg.Ctrl = Ctrl{
		Repeat:   buffer[ctrl]&(1<<repeatOffset) == 0,
		Priority: Priority(buffer[ctrl] >> priorityOffset & 0x03),
	}
	tg.Src = binary.BigEndian.Uint16(buffer[src1:dest1])
	tg.Dest = binary.BigEndian.Uint16(buffer[dest1:npci])
	tg.Npci = Npci{
		AddrType: AddrType(buffer[npci] >> addrTypeOffset),
		Routing:  Routing(buffer[npci] >> routingOffset & routingMask),
		Length:   Length(length),
	}
	tg.Tpci = Tpci{
		Transport: transport,
		Sequence:  Sequence(buffer[tpci] >> sequenceOffset & sequenceMask),
	}
	tg.Apci = Apci(buffer[tpci]&0x03<<apci1NegOffset | buffer[apci]>>apci2Offset)
	if length == 0 {
		// The APCI octet is the checksum: Pack drops the low APCI bits.
		tg.Apci &^= 0x03
		tg.Data = nil
		return nil
	}
	tg.Data = append(tg.Data[:0], buffer[apci:apci+length]...)
	tg.Data[0] &^= 0x03 << apci2Offset
	return nil
}

func repeat2uint8(r Repeat) uint8 {
	if r {
		return 0
	}
	return 1
}

func checksum(buffer []byte) uint8 {
	var checksum uint8 = 0xff
	for _, x := range buffer {
		checksum ^= x
	}
	return checksum
}

var priorityNames = [...]string{"system", "high", "alarm", "low"}

func (p Priority) String() string {
	if int(p) < len(priorityNames) {
		return priorityNames[p]
	}
	return "priority " + strconv.Itoa(int(p))
}

var apciNames = [...]string{
	"GroupValueRead", "GroupValueResponse", "GroupValueWrite",
	"IndividualAddrWrite", "IndividualAddrRequest", "IndividualAddrResponse",
	"AdcRead", "AdcResponse",
	"MemoryRead", "MemoryResponse", "MemoryWrite",
	"UserMessage", "MaskVersionRead", "MaskVersionResponse",
	"Restart", "Escape",
}

func (a Apci) String() string {
	if int(a) < len(apciNames) {
		return apciNames[a]
	}
	return "APCI " + strconv.Itoa(int(a))
}
