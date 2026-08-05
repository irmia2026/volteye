package wechatdb

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestDecryptDBRoundTripByteExact(t *testing.T) {
	dir := t.TempDir()
	rawKey := make([]byte, KeySize)
	rand.Read(rawKey)

	plain := emptyDBFixture(t)
	enc := filepath.Join(dir, "enc.db")
	encryptDBForTest(t, rawKey, plain, enc)

	dec := filepath.Join(dir, "dec.db")
	pages, err := DecryptDB(rawKey, enc, dec)
	if err != nil {
		t.Fatal(err)
	}
	if pages != len(plain)/PageSize {
		t.Fatalf("expected %d pages, got %d", len(plain)/PageSize, pages)
	}
	got, err := os.ReadFile(dec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("decrypted db must be byte-identical to source plaintext")
	}
}

func TestDecryptDBWrongKeyLeavesNoOutput(t *testing.T) {
	dir := t.TempDir()
	rawKey := make([]byte, KeySize)
	rand.Read(rawKey)

	plain := emptyDBFixture(t)
	enc := filepath.Join(dir, "enc.db")
	encryptDBForTest(t, rawKey, plain, enc)

	wrongKey := make([]byte, KeySize)
	rand.Read(wrongKey)
	dec := filepath.Join(dir, "dec.db")
	if _, err := DecryptDB(wrongKey, enc, dec); err == nil {
		t.Fatal("expected HMAC failure with wrong key")
	}
	if _, err := os.Stat(dec); !os.IsNotExist(err) {
		t.Fatal("failed decrypt must not leave output file")
	}
}

func TestDecryptDBRejectsUnaligned(t *testing.T) {
	dir := t.TempDir()
	rawKey := make([]byte, KeySize)
	rand.Read(rawKey)
	enc := filepath.Join(dir, "enc.db")
	if err := os.WriteFile(enc, make([]byte, PageSize+100), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptDB(rawKey, enc, filepath.Join(dir, "dec.db")); err == nil {
		t.Fatal("expected unaligned size error")
	}
}
