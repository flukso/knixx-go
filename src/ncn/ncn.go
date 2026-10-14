package ncn

import (
	"errors"
	"io"
)

// Services from the host controller to the NCN5121.
const (
	U_ResetReq       = 0x01
	U_StateReq       = 0x02
	U_SetBusyReq     = 0x03
	U_QuitBusyReq    = 0x04
	U_BusmonReq      = 0x05
	U_SystemStatReq  = 0x0d
	U_L_DataStartReq = 0x80
	U_L_DataEndReq   = 0x40
)

// maxFrameLen is the longest frame the 6 index bits of U_L_DataCont.req
// can address without U_L_DataOffset.req.
const maxFrameLen = 64

// TxData sends buf as a single request so that no gaps open up between the
// control and data bytes.
func TxData(w io.Writer, buf []byte) (int, error) {
	var req [2 * maxFrameLen]byte
	if len(buf) < 2 || len(buf) > maxFrameLen {
		return 0, errors.New("ncn: invalid frame length")
	}
	for i, b := range buf {
		offset := U_L_DataStartReq
		if i == len(buf)-1 {
			offset = U_L_DataEndReq
		}
		req[2*i] = uint8(offset + i)
		req[2*i+1] = b
	}
	if _, err := w.Write(req[:2*len(buf)]); err != nil {
		return 0, err
	}
	return len(buf), nil
}
