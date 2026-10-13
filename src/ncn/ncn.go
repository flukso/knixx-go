package ncn

import (
	"io"
)

const (
	U_ResetReq       = 0x01
	U_StateReq       = 0x02
	U_SetBusyReq     = 0x03
	U_QuitBusyReq    = 0x04
	U_BusmonReq      = 0x05
	U_L_DataStartReq = 0x80
	U_L_DataEndReq   = 0x40
)

func TxData(w io.Writer, buf []byte) (int, error) {
	var offset int
	for i, b := range buf {
		if i == len(buf)-1 {
			offset = U_L_DataEndReq
		} else {
			offset = U_L_DataStartReq
		}
		if _, err := w.Write([]byte{uint8(offset + i), b}); err != nil {
			return i, err
		}
	}
	return len(buf), nil
}
