package wechatdb

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

func ParseKeyHex(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid key hex: %v", err)
	}
	if len(b) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes (%d hex chars), got %d bytes", KeySize, KeySize*2, len(b))
	}
	return b, nil
}

func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
