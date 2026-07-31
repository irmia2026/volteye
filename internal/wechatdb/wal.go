package wechatdb

import (
	"encoding/binary"
	"fmt"
	"os"
)

const (
	walMagicLE    = 0x377f0682
	walMagicBE    = 0x377f0683
	walHeaderSize = 32
	walFrameHdr   = 24
)

func walChecksum(data []byte, s1, s2 uint32, bigEndian bool) (uint32, uint32) {
	for i := 0; i+8 <= len(data); i += 8 {
		var x0, x1 uint32
		if bigEndian {
			x0 = binary.BigEndian.Uint32(data[i:])
			x1 = binary.BigEndian.Uint32(data[i+4:])
		} else {
			x0 = binary.LittleEndian.Uint32(data[i:])
			x1 = binary.LittleEndian.Uint32(data[i+4:])
		}
		s1 += x0 + s2
		s2 += x1 + s1
	}
	return s1, s2
}

func DecryptWAL(rawKey []byte, encMainPath, walPath, dstPath string) (int, error) {
	mainFirst, err := os.ReadFile(encMainPath)
	if err != nil {
		return 0, err
	}
	if len(mainFirst) < PageSize {
		return 0, fmt.Errorf("main db too small for salt")
	}
	encKey := DeriveEncKey(rawKey, mainFirst[:SaltSize])

	data, err := os.ReadFile(walPath)
	if err != nil {
		return 0, err
	}
	if len(data) < walHeaderSize {
		return 0, fmt.Errorf("wal too small: %d bytes", len(data))
	}
	magic := binary.BigEndian.Uint32(data[0:4])
	var bigEndian bool
	switch magic {
	case walMagicLE:
		bigEndian = false
	case walMagicBE:
		bigEndian = true
	default:
		return 0, fmt.Errorf("unsupported wal magic 0x%08x", magic)
	}
	pageSize := int(binary.BigEndian.Uint32(data[8:12]))
	if pageSize != PageSize {
		return 0, fmt.Errorf("unexpected wal page size %d", pageSize)
	}

	out := make([]byte, len(data))
	copy(out, data)

	s1, s2 := walChecksum(out[0:24], 0, 0, bigEndian)
	frames := 0
	pos := walHeaderSize
	for pos+walFrameHdr+pageSize <= len(data) {
		hdr := data[pos : pos+walFrameHdr]
		page := data[pos+walFrameHdr : pos+walFrameHdr+pageSize]
		pn := binary.BigEndian.Uint32(hdr[0:4])
		plain, err := DecryptPage(encKey, page, pn)
		if err != nil {
			return frames, fmt.Errorf("wal frame %d page %d: %w", frames, pn, err)
		}
		copy(out[pos+walFrameHdr:], plain)
		s1, s2 = walChecksum(out[pos:pos+8], s1, s2, bigEndian)
		s1, s2 = walChecksum(out[pos+walFrameHdr:pos+walFrameHdr+pageSize], s1, s2, bigEndian)
		binary.BigEndian.PutUint32(out[pos+16:], s1)
		binary.BigEndian.PutUint32(out[pos+20:], s2)
		frames++
		pos += walFrameHdr + pageSize
	}
	if err := os.WriteFile(dstPath, out, 0644); err != nil {
		return frames, err
	}
	return frames, nil
}
