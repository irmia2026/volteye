package cleanup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"volteye/internal/store"
)

func TestArchiveAll(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.InsertMessages([]store.Message{
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 1, CreateTime: 1000, Content: "甲"},
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 2, CreateTime: 1001, Content: "乙"},
	}); err != nil {
		t.Fatal(err)
	}

	n, path, err := ArchiveAll(st, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 archived, got %d", n)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archive file missing: %v", err)
	}

	n2, path2, err := ArchiveAll(st, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 2 || path2 == path {
		t.Fatalf("same-second archive must not overwrite, got %s vs %s", path, path2)
	}
}

func TestArchiveAllEmpty(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	n, path, err := ArchiveAll(st, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || path != "" {
		t.Fatalf("empty store should produce no archive, got %d %s", n, path)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "archive"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".xlsx") {
			t.Fatalf("unexpected archive file %s", e.Name())
		}
	}
}
