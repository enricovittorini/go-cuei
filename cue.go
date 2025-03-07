package cuei

import (
	"fmt"
	"math/big"
)

/*

Cue is a SCTE35 cue.

A Cue contains:

		1 InfoSection
	   	1 Command
	   	1 Dll  Descriptor loop length
	   	0 or more Splice Descriptors
	   	1 Crc32
	   	0 or 1 packetData (if parsed from MPEGTS)
*/
type Cue struct {
	InfoSection *InfoSection
	Command     *Command
	Dll         uint16       `json:"DescriptorLoopLength"`
	Descriptors []Descriptor `json:",omitempty"`
	PacketData  *packetData  `json:",omitempty"`
	Crc32       uint32
}

// Decode takes Cue data as  []byte, base64 or hex string.
func (cue *Cue) Decode(i interface{}) bool {
	switch i.(type) {
	case string:
		str := i.(string)
		j := new(big.Int)
		_, err := fmt.Sscan(str, j)
		if err != nil {
			return cue.decodeBytes(decB64(str))
		}
		return cue.decodeBytes(j.Bytes())

	default:
		return cue.decodeBytes(i.([]byte))
	}
}

// decodeBytes extracts bits for the Cue values.
func (cue *Cue) decodeBytes(bites []byte) bool {
	var bd bitDecoder
	bd.load(bites)
	cue.InfoSection = &InfoSection{}
	if cue.InfoSection.Decode(&bd) {
		cue.Command = &Command{}
		cue.Command.Decode(cue.InfoSection.CommandType, &bd)
		cue.Dll = bd.uInt16(16)
		cue.dscptrLoop(cue.Dll, &bd)
		cue.Crc32 = bd.uInt32(32)
		return true
	}
	return false
}

// DscptrLoop loops over any splice descriptors
func (cue *Cue) dscptrLoop(dll uint16, bd *bitDecoder) {
	var i uint16
	i = 0
	l := dll
	for i < l {
		tag := bd.uInt8(8)
		i++
		length := bd.uInt16(8)
		i++
		i += length
		var sdr Descriptor
		sdr.Decode(bd, tag, uint8(length))
		cue.Descriptors = append(cue.Descriptors, sdr)
	}
}

func (cue *Cue) rollLoop() []byte {
	be := &bitEncoder{}
	be.Add(1, 8) //bumper
	for _, dscptr := range cue.Descriptors {
		bf := &bitEncoder{}
		bf.Add(1, 8) //bumper to keep leading zeros
		dscptr.Encode(bf)
		be.Add(dscptr.Tag, 8)
		// +3 is  +4 for identifier and -1 for the bumper.
		be.Add(len(bf.Bites.Bytes())+3, 8)
		be.AddBytes([]byte("CUEI"), 32)
		dscptr.Encode(be)
	}
	cue.Dll = uint16(len(be.Bites.Bytes()) - 1)
	return be.Bites.Bytes()[1:]
}

// Show display SCTE-35 data as JSON.
func (cue *Cue) Show() {
	fmt.Println(mkJson(&cue))
}

// AdjustPts adds seconds to cue.InfoSection.PtsAdjustment
func (cue *Cue) AdjustPts(seconds float64) {
	cue.InfoSection.PtsAdjustment += seconds
	cue.Encode()
}

// Encode Cue currently works for Splice Inserts and Time Signals
func (cue *Cue) Encode() []byte {
	cmdb := cue.Command.Encode()
	cmdl := len(cmdb)
	cue.InfoSection.CommandLength = uint16(cmdl)
	cue.InfoSection.CommandType = cue.Command.CommandType
	// 11 bytes for info section + command + 2 descriptor loop length
	// + descriptor loop + 4 for crc
	cue.InfoSection.SectionLength = uint16(11+cmdl+2+4) + cue.Dll
	isecb := cue.InfoSection.Encode()
	be := &bitEncoder{}
	isecbits := uint(len(isecb) << 3)
	be.AddBytes(isecb, isecbits)
	cmdbits := uint(cmdl << 3)
	be.AddBytes(cmdb, cmdbits)
	dloop := cue.rollLoop()
	be.Add(cue.Dll, 16)
	be.AddBytes(dloop, uint(cue.Dll<<3))
	cue.Crc32 = cRC32(be.Bites.Bytes())
	be.Add(cue.Crc32, 32)
	return be.Bites.Bytes()
}

// Encode2B64 Encodes cue and returns Base64 string
func (cue *Cue) Encode2B64() string {
	return encB64(cue.Encode())
}

// Encode2Hex encodes cue and returns as a hex string
func (cue *Cue) Encode2Hex() string {
	b := new(big.Int)
	b.SetBytes(cue.Encode())
	return fmt.Sprintf("0x%v", b.Text(16))
}

// used by Six2Five to convert a time signal to a splice insert
func (cue *Cue) mkSpliceInsert() {
	cue.Command.CommandType = 5
	cue.Command.Name = "Splice Insert"
	cue.InfoSection.CommandType = 5
	cue.Command.ProgramSpliceFlag = true
	cue.Command.SpliceEventCancelIndicator = false
	cue.Command.OutOfNetworkIndicator = false
	cue.Command.TimeSpecifiedFlag = false
	cue.Command.DurationFlag = false
	cue.Command.BreakAutoReturn = false
	cue.Command.SpliceImmediateFlag = false
	cue.Command.AvailNum = 0
	cue.Command.AvailExpected = 0
	if cue.Command.PTS > 0.0 {
		cue.Command.TimeSpecifiedFlag = true
		cue.Command.PTS = cue.Command.PTS
	}
}

/*
	Convert  Cue.Command  from a  Time Signal
	to a Splice Insert and return a base64 string
*/
func (cue *Cue) Six2Five() string {
	segStarts := []uint16{0x22, 0x30, 0x32, 0x34, 0x36, 0x38, 0x3a, 0x3c, 0x3e, 0x44, 0x46}
	segStops := []uint16{0x23, 0x31, 0x33, 0x35, 0x37, 0x39, 0x3b, 0x3d, 0x3f, 0x45, 0x47}
	if cue.InfoSection.CommandType == 6 {
		for _, dscptr := range cue.Descriptors {
			if dscptr.Tag == 2 {
				//value, _ := strconv.ParseInt(hex, 16, 64)
				cue.Command.SpliceEventID = uint32(hex2Int(dscptr.SegmentationEventID))
				if isIn(segStarts, uint16(dscptr.SegmentationTypeID)) {
					if dscptr.SegmentationDurationFlag {
						cue.mkSpliceInsert()
						cue.Command.OutOfNetworkIndicator = true
						cue.Command.DurationFlag = true
						cue.Command.BreakAutoReturn = true
						cue.Command.BreakDuration = dscptr.SegmentationDuration
					}
				} else {
					if isIn(segStops, uint16(dscptr.SegmentationTypeID)) {
						cue.mkSpliceInsert()
					}
				}
			}
		}
	}
	return encB64(cue.Encode())
}

// initialize and return a *Cue
func NewCue() *Cue {
	cue := &Cue{}
	return cue
}
