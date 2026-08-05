package tray

import (
	"encoding/binary"
	"testing"
)

func TestMakeIconStructure(t *testing.T) {
	const size = 32
	ico := MakeIcon(size)
	maskStride := ((size + 31) / 32) * 4
	wantLen := 22 + 40 + size*size*4 + maskStride*size
	if len(ico) != wantLen {
		t.Fatalf("ico length = %d, want %d", len(ico), wantLen)
	}
	if binary.LittleEndian.Uint16(ico[0:]) != 0 || binary.LittleEndian.Uint16(ico[2:]) != 1 || binary.LittleEndian.Uint16(ico[4:]) != 1 {
		t.Fatal("bad ICONDIR header")
	}
	if ico[6] != size || ico[7] != size {
		t.Fatal("bad icon entry dimensions")
	}
	if binary.LittleEndian.Uint32(ico[14:]) != uint32(wantLen-22) {
		t.Fatal("bad image size in entry")
	}
	if binary.LittleEndian.Uint32(ico[22:]) != 40 {
		t.Fatal("bad BITMAPINFOHEADER size")
	}
	if binary.LittleEndian.Uint32(ico[30:]) != uint32(size*2) {
		t.Fatal("icon bitmap height must be doubled for mask")
	}

	opaque := 0
	pix := ico[22+40:]
	for i := 0; i < size*size; i++ {
		if pix[i*4+3] == 0xff {
			opaque++
		}
	}
	if opaque < size*size/2 {
		t.Fatalf("icon mostly transparent: %d/%d opaque", opaque, size*size)
	}
}

func TestMakeIconDefaultSize(t *testing.T) {
	if len(MakeIcon(0)) == 0 {
		t.Fatal("zero size should fall back to default")
	}
}
