package legacywire

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestDecodePropUsePreservesTargetOrderAndDuplicates(t *testing.T) {
	prop := []byte("flower")
	targets := []uint32{1002, 1002, 1001}
	data := make([]byte, 52+len(prop)+4*len(targets))
	encodeHeader(data, bsHeader{Type: messageGameBroadcastUseProp, Length: uint32(len(data))})
	binary.LittleEndian.PutUint32(data[24:28], 3)
	binary.LittleEndian.PutUint32(data[28:32], 1001)
	binary.LittleEndian.PutUint32(data[32:36], 52)
	binary.LittleEndian.PutUint32(data[36:40], uint32(len(prop)))
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(targets)))
	binary.LittleEndian.PutUint32(data[44:48], uint32(52+len(prop)))
	binary.LittleEndian.PutUint32(data[48:52], uint32(4*len(targets)))
	copy(data[52:], prop)
	for index, target := range targets {
		binary.LittleEndian.PutUint32(data[52+len(prop)+index*4:], target)
	}
	got, err := DecodePropUse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.SenderID != 1001 || got.SendCount != 3 || got.PropID != "flower" || !reflect.DeepEqual(got.TargetIDs, targets) {
		t.Fatalf("prop = %#v", got)
	}
}
