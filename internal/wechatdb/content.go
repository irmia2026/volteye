package wechatdb

import (
	"bytes"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

var (
	zstdDec     *zstd.Decoder
	zstdDecOnce sync.Once
)

func zstdDecompress(data []byte) ([]byte, bool) {
	zstdDecOnce.Do(func() {
		zstdDec, _ = zstd.NewReader(nil)
	})
	if zstdDec == nil {
		return nil, false
	}
	out, err := zstdDec.DecodeAll(data, nil)
	if err != nil {
		return nil, false
	}
	return out, true
}

func toBytes(v any) []byte {
	switch t := v.(type) {
	case []byte:
		return t
	case string:
		return []byte(t)
	default:
		return nil
	}
}

func DecodeContent(compress, message any) string {
	cb := toBytes(compress)
	if len(cb) > len(zstdMagic) && bytes.HasPrefix(cb, zstdMagic) {
		if out, ok := zstdDecompress(cb); ok {
			return string(out)
		}
	}
	mb := toBytes(message)
	if len(mb) > len(zstdMagic) && bytes.HasPrefix(mb, zstdMagic) {
		if out, ok := zstdDecompress(mb); ok {
			return string(out)
		}
	}
	if len(mb) > 0 {
		return string(mb)
	}
	if len(cb) > 0 {
		return string(cb)
	}
	return ""
}

func StripSenderPrefix(content, senderWxid string) string {
	if senderWxid == "" {
		return content
	}
	if strings.HasPrefix(content, senderWxid+":") {
		return strings.TrimLeft(content[len(senderWxid)+1:], " \n")
	}
	return content
}
