package wechatdb

import (
	"bytes"
	"fmt"
	"os"
)

var movRdxImm64 = []byte{0x48, 0xBA}

func ExtractInternalKeys(dllPath string) ([][]byte, error) {
	data, err := os.ReadFile(dllPath)
	if err != nil {
		return nil, err
	}
	seen := map[[KeySize]byte]struct{}{}
	var keys [][]byte
	for i := 0; i+10 <= len(data); {
		rel := bytes.Index(data[i:], movRdxImm64)
		if rel < 0 {
			break
		}
		start := i + rel
		var key [KeySize]byte
		end, ok := matchKeyStub(data, start, 0, 0, &key)
		if ok {
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				kk := make([]byte, KeySize)
				copy(kk, key[:])
				keys = append(keys, kk)
			}
			i = start + end
		} else {
			i = start + 2
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no internal key stub found in %s", dllPath)
	}
	return keys, nil
}

func matchKeyStub(buf []byte, p, stage, used int, key *[KeySize]byte) (int, bool) {
	if p+10 > len(buf) || buf[p] != 0x48 || buf[p+1] != 0xBA {
		return 0, false
	}
	copy(key[used:used+8], buf[p+2:p+10])
	p += 10
	if stage == 3 {
		for gap := 3; gap <= 8; gap++ {
			if p+gap+3 <= len(buf) && buf[p+gap] == 0x48 && buf[p+gap+1] == 0x85 && buf[p+gap+2] == 0xC0 {
				return p + gap + 3, true
			}
		}
		return 0, false
	}
	for gap := 3; gap <= 8; gap++ {
		if p+gap+2 <= len(buf) && buf[p+gap] == 0x48 && buf[p+gap+1] == 0xBA {
			if end, ok := matchKeyStub(buf, p+gap, stage+1, used+8, key); ok {
				return end, true
			}
		}
	}
	return 0, false
}

func XorKeyVariant(raw, mask []byte) []byte {
	if len(raw) != KeySize || len(mask) != KeySize {
		return nil
	}
	out := make([]byte, KeySize)
	for i := range out {
		out[i] = raw[i] ^ mask[i]
	}
	return out
}
