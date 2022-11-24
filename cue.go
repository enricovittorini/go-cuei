package cuei

import (
	"fmt"
	gobs "github.com/futzu/gob"
)

// Cue a SCTE35 cue.
type Cue struct {
	InfoSection
	Command     SpliceCommand
	Descriptors []SpliceDescriptor `json:",omitempty"`
	PacketData      *packetData    `json:",omitempty"`
}

// Decode extracts bits for the Cue values.
func (cue *Cue) Decode(bites []byte) bool {
	var gob gobs.Gob
	gob.Load(bites)
	if cue.InfoSection.Decode(&gob) {
		cue.Command.Decode(cue.InfoSection.SpliceCommandType, &gob)
		cue.InfoSection.DescriptorLoopLength = gob.UInt16(16)
		cue.dscptrLoop(&gob)
		return true
	}
	return false
}

// DscptrLoop loops over any splice descriptors
func (cue *Cue) dscptrLoop(gob *gobs.Gob) {
	var i uint16
	i = 0
	l := cue.InfoSection.DescriptorLoopLength
	for i < l {
		tag := gob.UInt8(8)
		i++
		length := gob.UInt16(8)
		i++
		i += length
		var sdr SpliceDescriptor
		sdr.Decode(gob, tag, uint8(length))
		cue.Descriptors = append(cue.Descriptors, sdr)
	}
}

// Show display SCTE-35 data as JSON.
func (cue *Cue) Show() {
	fmt.Println(MkJson(&cue))
}
