package bus

import "strconv"

// The append functions render parts of a sniffer line into a buffer, without
// allocating while the buffer has room.

const hexDigits = "0123456789abcdef"

// appendDec appends v in decimal.
func appendDec(dst []byte, v uint64) []byte {
	return strconv.AppendUint(dst, v, 10)
}

// appendHex2 appends b as two hex digits.
func appendHex2(dst []byte, b byte) []byte {
	return append(dst, hexDigits[b>>4], hexDigits[b&0x0f])
}

// appendHexBytes appends p as hex octets separated by spaces, like the % x
// verb of fmt.
func appendHexBytes(dst []byte, p []byte) []byte {
	for i, b := range p {
		if i > 0 {
			dst = append(dst, ' ')
		}
		dst = appendHex2(dst, b)
	}
	return dst
}

// appendIndividualAddr appends an individual address as area.line.device.
func appendIndividualAddr(dst []byte, a uint16) []byte {
	dst = appendDec(dst, uint64(a>>12&0x0f))
	dst = append(dst, '.')
	dst = appendDec(dst, uint64(a>>8&0x0f))
	dst = append(dst, '.')
	return appendDec(dst, uint64(a&0xff))
}

// appendGroupAddr appends a group address as main/middle/sub.
func appendGroupAddr(dst []byte, a uint16) []byte {
	dst = appendDec(dst, uint64(a>>11&0x1f))
	dst = append(dst, '/')
	dst = appendDec(dst, uint64(a>>8&0x07))
	dst = append(dst, '/')
	return appendDec(dst, uint64(a&0xff))
}

// f16 decodes the DPT 9 value in data, the APCI octet and two value
// octets, in hundredths: the value is 0.01·m·2^e, so this is exact. ok is
// false for another length.
func f16(data []byte) (h int32, ok bool) {
	if len(data) != 3 {
		return 0, false
	}
	m := int32(data[1]&0x07)<<8 | int32(data[2])
	if data[1]&0x80 != 0 {
		m -= 2048
	}
	return m << (data[1] >> 3 & 0x0f), true
}

// appendHundredths appends h/100 with two decimals.
func appendHundredths(dst []byte, h int32) []byte {
	if h < 0 {
		dst = append(dst, '-')
		h = -h
	}
	dst = appendDec(dst, uint64(h/100))
	return append(dst, '.', byte('0'+h/10%10), byte('0'+h%10))
}

// The DPT 9 bounds, in hundredths.
const (
	minF16Pack = -671088_64
	maxF16Pack = 670760_96
)

// boolWrite returns a write of a DPT 1 value to group.
func boolWrite(group uint16, on bool) write {
	w := write{group: group, n: 1}
	if on {
		w.data[0] = 1
	}
	return w
}

// f16Write returns a write of the DPT 9 value h hundredths to group, after
// the APCI octet: the value is m·2^e hundredths with an 11-bit signed
// mantissa m, so m loses its low bits as e grows. A value out of range is
// clamped.
func f16Write(group uint16, h int32) write {
	h = min(max(h, minF16Pack), maxF16Pack)
	m, e := h, uint8(0)
	for m > 2047 || m < -2048 {
		m /= 2
		e++
	}
	w := write{group: group, n: 3}
	w.data[1] = e << 3
	if m < 0 {
		m += 2048
		w.data[1] |= 0x80
	}
	w.data[1] |= uint8(m>>8) & 0x07
	w.data[2] = uint8(m)
	return w
}
