package wechatdb

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"
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

// ReadSalt reads the 16-byte salt stored at the start of page 1 of an
// encrypted db. The salt is all that WAL decryption ever needs from the
// encrypted main db, so callers can avoid keeping a full encrypted copy.
func ReadSalt(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(f, salt); err != nil {
		return nil, fmt.Errorf("main db too small for salt")
	}
	return salt, nil
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
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if fi.Size() < PageSize || fi.Size()%PageSize != 0 {
		return 0, fmt.Errorf("db size %d not aligned to page size", fi.Size())
	}
	pages := int(fi.Size() / PageSize)

	page1 := make([]byte, PageSize)
	if _, err := io.ReadFull(in, page1); err != nil {
		return 0, fmt.Errorf("read page 1: %w", err)
	}
	salt := page1[:SaltSize]
	encKey := DeriveEncKey(rawKey, salt)
	macKey := deriveMacKey(encKey, salt)
	if !hmac.Equal(pageHMAC(macKey, page1, 1), page1[PageSize-HMACSize:PageSize]) {
		return 0, fmt.Errorf("page-1 HMAC mismatch: wrong key or corrupted db")
	}

	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(dst)
		}
	}()

	plain1, err := DecryptPage(encKey, page1, 1)
	if err != nil {
		return 0, fmt.Errorf("page 1: %w", err)
	}
	if _, err := out.Write(plain1); err != nil {
		return 0, err
	}
	page := make([]byte, PageSize)
	for p := 2; p <= pages; p++ {
		if _, err := io.ReadFull(in, page); err != nil {
			return 0, fmt.Errorf("read page %d: %w", p, err)
		}
		plain, err := DecryptPage(encKey, page, uint32(p))
		if err != nil {
			return 0, fmt.Errorf("page %d: %w", p, err)
		}
		if _, err := out.Write(plain); err != nil {
			return 0, err
		}
	}
	if err := out.Sync(); err != nil {
		return 0, err
	}
	ok = true
	return pages, nil
}
