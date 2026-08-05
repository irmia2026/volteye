package tray

import (
	"bytes"
	"encoding/binary"
	"math"
)

func pointInPoly(x, y float64, poly [][2]float64) bool {
	inside := false
	j := len(poly) - 1
	for i := 0; i < len(poly); i++ {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}

func MakeIcon(size int) []byte {
	if size <= 0 {
		size = 32
	}
	bg := [3]byte{0x1c, 0x27, 0x33}
	accent := [3]byte{0x79, 0xb8, 0xd8}
	bolt := [][2]float64{
		{0.60, 0.06}, {0.28, 0.56}, {0.45, 0.56}, {0.36, 0.94}, {0.74, 0.42}, {0.55, 0.42},
	}
	radius := float64(size) * 0.22

	rgba := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			i := (y*size + x) * 4
			inRect := true
			cx := math.Min(fx, float64(size)-fx)
			cy := math.Min(fy, float64(size)-fy)
			if cx < radius && cy < radius {
				dx, dy := radius-cx, radius-cy
				if dx*dx+dy*dy > radius*radius {
					inRect = false
				}
			}
			if !inRect {
				continue
			}
			c := bg
			if pointInPoly(fx/float64(size), fy/float64(size), bolt) {
				c = accent
			}
			rgba[i], rgba[i+1], rgba[i+2], rgba[i+3] = c[0], c[1], c[2], 0xff
		}
	}

	maskStride := ((size + 31) / 32) * 4
	imgSize := 40 + size*size*4 + maskStride*size

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	buf.WriteByte(byte(size))
	buf.WriteByte(byte(size))
	buf.WriteByte(0)
	buf.WriteByte(0)
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(32))
	binary.Write(&buf, binary.LittleEndian, uint32(imgSize))
	binary.Write(&buf, binary.LittleEndian, uint32(22))

	binary.Write(&buf, binary.LittleEndian, uint32(40))
	binary.Write(&buf, binary.LittleEndian, int32(size))
	binary.Write(&buf, binary.LittleEndian, int32(size*2))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(32))
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	binary.Write(&buf, binary.LittleEndian, int32(0))
	binary.Write(&buf, binary.LittleEndian, int32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			buf.WriteByte(rgba[i+2])
			buf.WriteByte(rgba[i+1])
			buf.WriteByte(rgba[i])
			buf.WriteByte(rgba[i+3])
		}
	}
	buf.Write(make([]byte, maskStride*size))
	return buf.Bytes()
}
