package wechatdb

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

func DeriveEncKey(rawKey, salt []byte) []byte {
	return pbkdf2.Key(rawKey, salt, RoundCount, KeySize, sha512.New)
}

func deriveMacKey(encKey, salt []byte) []byte {
	macSalt := make([]byte, len(salt))
	for i, b := range salt {
		macSalt[i] = b ^ 0x3a
	}
	return pbkdf2.Key(encKey, macSalt, 2, KeySize, sha512.New)
}

func pageHMAC(macKey, page []byte, pageNum uint32) []byte {
	start := 0
	if pageNum == 1 {
		start = SaltSize
	}
	mac := hmac.New(sha512.New, macKey)
	mac.Write(page[start : PageSize-ReserveSize+IVSize])
	var pn [4]byte
	binary.LittleEndian.PutUint32(pn[:], pageNum)
	mac.Write(pn[:])
	return mac.Sum(nil)
}

func VerifyPage1Key(rawKey, page1 []byte) bool {
	if len(rawKey) != KeySize || len(page1) < PageSize {
		return false
	}
	salt := page1[:SaltSize]
	encKey := DeriveEncKey(rawKey, salt)
	macKey := deriveMacKey(encKey, salt)
	expected := pageHMAC(macKey, page1, 1)
	stored := page1[PageSize-HMACSize : PageSize]
	return hmac.Equal(expected, stored)
}

func DecryptPage(encKey, page []byte, pageNum uint32) ([]byte, error) {
	if len(page) != PageSize {
		return nil, fmt.Errorf("bad page size %d", len(page))
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	iv := page[PageSize-ReserveSize : PageSize-ReserveSize+IVSize]
	offset := 0
	if pageNum == 1 {
		offset = SaltSize
	}
	src := page[offset : PageSize-ReserveSize]
	out := make([]byte, PageSize)
	if pageNum == 1 {
		copy(out, SQLiteHeader)
	}
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out[offset:PageSize-ReserveSize], src)
	return out, nil
}

func DecryptDB(rawKey []byte, src, dst string) (int, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	if len(data) < PageSize || len(data)%PageSize != 0 {
		return 0, fmt.Errorf("db size %d not aligned to page size", len(data))
	}
	salt := data[:SaltSize]
	encKey := DeriveEncKey(rawKey, salt)
	macKey := deriveMacKey(encKey, salt)
	if !hmac.Equal(pageHMAC(macKey, data[:PageSize], 1), data[PageSize-HMACSize:PageSize]) {
		return 0, fmt.Errorf("page-1 HMAC mismatch: wrong key or corrupted db")
	}
	pages := len(data) / PageSize
	out := make([]byte, len(data))
	for p := 1; p <= pages; p++ {
		page := data[(p-1)*PageSize : p*PageSize]
		plain, err := DecryptPage(encKey, page, uint32(p))
		if err != nil {
			return 0, fmt.Errorf("page %d: %w", p, err)
		}
		copy(out[(p-1)*PageSize:], plain)
	}
	if err := os.WriteFile(dst, out, 0644); err != nil {
		return 0, err
	}
	return pages, nil
}
