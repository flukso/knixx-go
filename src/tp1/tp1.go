package tp1

import (
	"encoding/binary"
	"errors"
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

func (tg *Telegram) Pack() ([]byte, error) {
	var buffer = make([]byte, len(tg.Data)+8)
	if len(tg.Data) > maxPayloadLength {
		return nil, errors.New("tp1: max payload length exceeded")
	}
	buffer[ctrl] = ctrlDefault |
		(repeat2uint8(tg.Repeat) << repeatOffset) |
		(uint8(tg.Priority) << priorityOffset)
	binary.BigEndian.PutUint16(buffer[src1:dest1], tg.Src)
	binary.BigEndian.PutUint16(buffer[dest1:npci], tg.Dest)
	buffer[npci] = (uint8(tg.AddrType) << addrTypeOffset) |
		((uint8(tg.Routing) & routingMask) << routingOffset) |
		uint8(len(tg.Data))
	if tg.Transport > NumberedDataPacket {
		return nil, errors.New("tp1: control data transports not yet supported")
	}
	buffer[tpci] = (uint8(tg.Transport) << transportOffset) |
		((uint8(tg.Sequence) & sequenceMask) << sequenceOffset) |
		(uint8(tg.Apci) >> apci1NegOffset)
	copy(buffer[apci:], tg.Data)
	buffer[apci] |= (uint8(tg.Apci) << apci2Offset)
	buffer[len(buffer)-1] = checksum(buffer)

	return buffer, nil
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
