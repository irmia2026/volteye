package wechatdb

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func encryptPageForTest(encKey, plainPage []byte, pageNum uint32, salt []byte) []byte {
	out := make([]byte, PageSize)
	offset := 0
	if pageNum == 1 {
		copy(out, salt)
		offset = SaltSize
	}
	iv := make([]byte, IVSize)
	rand.Read(iv)
	block, _ := aes.NewCipher(encKey)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[offset:PageSize-ReserveSize], plainPage[offset:PageSize-ReserveSize])
	copy(out[PageSize-ReserveSize:], iv)
	macKey := deriveMacKey(encKey, salt)
	mac := hmac.New(sha512.New, macKey)
	mac.Write(out[offset : PageSize-ReserveSize+IVSize])
	var pn [4]byte
	binary.LittleEndian.PutUint32(pn[:], pageNum)
	mac.Write(pn[:])
	copy(out[PageSize-HMACSize:], mac.Sum(nil))
	return out
}

func encryptDBForTest(t *testing.T, rawKey, plain []byte, dst string) []byte {
	t.Helper()
	salt := make([]byte, SaltSize)
	rand.Read(salt)
	encKey := DeriveEncKey(rawKey, salt)
	pages := len(plain) / PageSize
	out := make([]byte, len(plain))
	for p := 1; p <= pages; p++ {
		enc := encryptPageForTest(encKey, plain[(p-1)*PageSize:p*PageSize], uint32(p), salt)
		copy(out[(p-1)*PageSize:], enc)
	}
	if err := os.WriteFile(dst, out, 0644); err != nil {
		t.Fatal(err)
	}
	return salt
}

func emptyDBFixture(t *testing.T) []byte {
	t.Helper()
	plain := make([]byte, 2*PageSize)
	page1 := plain[:PageSize]
	copy(page1, SQLiteHeader)
	binary.BigEndian.PutUint16(page1[16:18], PageSize)
	page1[18] = 2
	page1[19] = 2
	page1[20] = 0
	page1[21] = 64
	page1[22] = 32
	page1[23] = 32
	binary.BigEndian.PutUint32(page1[24:28], 1)
	binary.BigEndian.PutUint32(page1[28:32], 2)
	binary.BigEndian.PutUint32(page1[40:44], 1)
	binary.BigEndian.PutUint32(page1[44:48], 4)
	binary.BigEndian.PutUint32(page1[56:60], 1)
	binary.BigEndian.PutUint32(page1[92:96], 1)
	binary.BigEndian.PutUint32(page1[96:100], 3045000)
	page1[100] = 0x0d
	binary.BigEndian.PutUint16(page1[105:107], PageSize-ReserveSize)
	page2 := plain[PageSize:]
	page2[0] = 0x0d
	binary.BigEndian.PutUint16(page2[5:7], PageSize-ReserveSize)
	return plain
}

func buildWalForTest(salt1, salt2 uint32, frames [][]byte, frameSalts [][2]uint32, pageNo, dbPages uint32) []byte {
	out := make([]byte, walHeaderSize+len(frames)*(walFrameHdr+PageSize))
	binary.BigEndian.PutUint32(out[0:], walMagicLE)
	binary.BigEndian.PutUint32(out[4:], 3007000)
	binary.BigEndian.PutUint32(out[8:], PageSize)
	binary.BigEndian.PutUint32(out[12:], 1)
	binary.BigEndian.PutUint32(out[16:], salt1)
	binary.BigEndian.PutUint32(out[20:], salt2)
	hc1, hc2 := walChecksum(out[:24], 0, 0, false)
	binary.BigEndian.PutUint32(out[24:], hc1)
	binary.BigEndian.PutUint32(out[28:], hc2)
	s1, s2 := hc1, hc2
	for i, page := range frames {
		off := walHeaderSize + i*(walFrameHdr+PageSize)
		binary.BigEndian.PutUint32(out[off:], pageNo)
		binary.BigEndian.PutUint32(out[off+4:], dbPages)
		binary.BigEndian.PutUint32(out[off+8:], frameSalts[i][0])
		binary.BigEndian.PutUint32(out[off+12:], frameSalts[i][1])
		copy(out[off+walFrameHdr:], page)
		s1, s2 = walChecksum(out[off:off+8], s1, s2, false)
		s1, s2 = walChecksum(page, s1, s2, false)
		binary.BigEndian.PutUint32(out[off+16:], s1)
		binary.BigEndian.PutUint32(out[off+20:], s2)
	}
	return out
}

func walLogFrames(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var busy, log, ckpt int
	if err := db.QueryRow(`PRAGMA wal_checkpoint(PASSIVE)`).Scan(&busy, &log, &ckpt); err != nil {
		t.Fatal(err)
	}
	return log
}

func TestDecryptWALGenerationFiltering(t *testing.T) {
	dir := t.TempDir()
	rawKey := make([]byte, KeySize)
	rand.Read(rawKey)

	plain := emptyDBFixture(t)
	encMain := filepath.Join(dir, "enc.db")
	salt := encryptDBForTest(t, rawKey, plain, encMain)
	encKey := DeriveEncKey(rawKey, salt)

	page2 := make([]byte, PageSize)
	copy(page2, plain[PageSize:2*PageSize])
	page2[100] = 0xff
	encPage2 := encryptPageForTest(encKey, page2, 2, salt)

	curSalt := [2]uint32{0x11112222, 0x33334444}
	oldSalt := [2]uint32{0x99998888, 0x77776666}

	dec := filepath.Join(dir, "dec.db")
	if _, err := DecryptDB(rawKey, encMain, dec); err != nil {
		t.Fatal(err)
	}

	t.Run("old_generation_only_produces_no_wal", func(t *testing.T) {
		wal := buildWalForTest(curSalt[0], curSalt[1], [][]byte{encPage2}, [][2]uint32{oldSalt}, 2, 2)
		src := filepath.Join(dir, "src-wal")
		os.WriteFile(src, wal, 0644)
		frames, err := DecryptWAL(rawKey, encMain, src, dec+"-wal")
		if err != nil {
			t.Fatal(err)
		}
		if frames != 0 {
			t.Fatalf("expected 0 current-gen frames, got %d", frames)
		}
		if _, err := os.Stat(dec + "-wal"); !os.IsNotExist(err) {
			t.Fatalf("stale dec wal should be removed")
		}
	})

	t.Run("current_generation_replays", func(t *testing.T) {
		mixed := buildWalForTest(curSalt[0], curSalt[1], [][]byte{encPage2, encPage2}, [][2]uint32{oldSalt, curSalt}, 2, 2)
		src := filepath.Join(dir, "src-wal2")
		os.WriteFile(src, mixed, 0644)
		frames, err := DecryptWAL(rawKey, encMain, src, dec+"-wal")
		if err != nil {
			t.Fatal(err)
		}
		if frames != 1 {
			t.Fatalf("expected 1 current-gen frame, got %d", frames)
		}
		if n := walLogFrames(t, dec); n != 1 {
			t.Fatalf("sqlite recognized %d wal frames, want 1", n)
		}
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dec)+"?_pragma=busy_timeout(2000)")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var cnt int
		if err := db.QueryRow(`SELECT COUNT(1) FROM sqlite_master`).Scan(&cnt); err != nil {
			t.Fatalf("post-replay query failed: %v", err)
		}
	})
}
