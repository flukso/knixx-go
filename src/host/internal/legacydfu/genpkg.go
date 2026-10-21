package legacydfu

import (
	"archive/zip"
	"encoding/binary"
	"encoding/json"
	"io"
	"time"
)

// InitPacket holds the fields of the legacy init packet, which the
// bootloader checks before it takes an image.
type InitPacket struct {
	DevType uint16   // the device type, 0x0052 for the Adafruit nRF52 boards
	DevRev  uint16   // the device revision, 0xFFFF for any
	AppVer  uint32   // the application version, 0xFFFFFFFF for any
	SDReq   []uint16 // the SoftDevice firmware ids the image runs on
}

// Marshal returns the init packet of an image with CRC crc, little endian:
// the device type and revision, the application version, the number of
// SoftDevice ids, the ids and the CRC.
func (ip InitPacket) Marshal(crc uint16) []byte {
	b := binary.LittleEndian.AppendUint16(nil, ip.DevType)
	b = binary.LittleEndian.AppendUint16(b, ip.DevRev)
	b = binary.LittleEndian.AppendUint32(b, ip.AppVer)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(ip.SDReq)))
	for _, sd := range ip.SDReq {
		b = binary.LittleEndian.AppendUint16(b, sd)
	}
	return binary.LittleEndian.AppendUint16(b, crc)
}

// CRC16 is the CRC of the bootloader's crc16_compute, CRC-16/CCITT-FALSE:
// polynomial 0x1021, initial value 0xFFFF.
func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc = crc>>8 | crc<<8
		crc ^= uint16(b)
		crc ^= (crc & 0xFF) >> 4
		crc ^= crc << 12
		crc ^= (crc & 0xFF) << 5
	}
	return crc
}

// NewPackage returns the package of the application image bin with the
// init packet fields ip, as adafruit-nrfutil dfu genpkg builds it.
func NewPackage(bin []byte, ip InitPacket) *Package {
	return &Package{Bin: bin, Dat: ip.Marshal(CRC16(bin))}
}

// The manifest.json of a genpkg package. The fields are in sorted order, as
// adafruit-nrfutil writes them.
type genManifest struct {
	Manifest struct {
		Application struct {
			BinFile        string `json:"bin_file"`
			DatFile        string `json:"dat_file"`
			InitPacketData struct {
				ApplicationVersion uint32   `json:"application_version"`
				DeviceRevision     uint16   `json:"device_revision"`
				DeviceType         uint16   `json:"device_type"`
				FirmwareCRC16      uint16   `json:"firmware_crc16"`
				SoftdeviceReq      []uint16 `json:"softdevice_req"`
			} `json:"init_packet_data"`
		} `json:"application"`
		DFUVersion float64 `json:"dfu_version"`
	} `json:"manifest"`
}

// WriteZip writes p as a DFU package to w: name.bin, name.dat and the
// manifest.json with the init packet fields ip, as adafruit-nrfutil dfu
// genpkg does.
func (p *Package) WriteZip(w io.Writer, name string, ip InitPacket) error {
	var m genManifest
	app := &m.Manifest.Application
	app.BinFile, app.DatFile = name+".bin", name+".dat"
	app.InitPacketData.ApplicationVersion = ip.AppVer
	app.InitPacketData.DeviceRevision = ip.DevRev
	app.InitPacketData.DeviceType = ip.DevType
	app.InitPacketData.FirmwareCRC16 = CRC16(p.Bin)
	app.InitPacketData.SoftdeviceReq = ip.SDReq
	m.Manifest.DFUVersion = 0.5
	manifest, err := json.MarshalIndent(m, "", "    ")
	if err != nil {
		return err
	}

	z := zip.NewWriter(w)
	now := time.Now()
	for _, f := range []struct {
		name string
		data []byte
	}{{app.BinFile, p.Bin}, {app.DatFile, p.Dat}, {"manifest.json", manifest}} {
		fw, err := z.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.data); err != nil {
			return err
		}
	}
	return z.Close()
}
