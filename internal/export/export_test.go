package export

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"volteye/internal/store"
)

func seedStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.UpsertGroup("g1@chatroom", "供电一群"); err != nil {
		t.Fatal(err)
	}
	msgs := []store.Message{
		{GroupWxid: "g1@chatroom", LocalID: 1, CreateTime: time.Now().Add(-48 * time.Hour).Unix(), SenderWxid: "u1", Content: "旧消息", LocalType: 1},
		{GroupWxid: "g1@chatroom", LocalID: 2, CreateTime: time.Now().Add(-2 * time.Hour).Unix(), SenderWxid: "u2", Content: "新工单：抢修", LocalType: 1},
		{GroupWxid: "g1@chatroom", LocalID: 3, CreateTime: time.Now().Add(-1 * time.Hour).Unix(), SenderWxid: "u3", Content: "普通聊天", LocalType: 1},
	}
	if _, err := st.InsertMessages(msgs); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddRule("工单", "工单", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkMessageScanned(1, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkMessageScanned(2, "1"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkMessageScanned(3, ""); err != nil {
		t.Fatal(err)
	}
	return st
}

func readRows(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestExportAll(t *testing.T) {
	st := seedStore(t)
	out := filepath.Join(t.TempDir(), "all.xlsx")
	n, err := MessagesXLSX(st, Options{}, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 rows, got %d", n)
	}
	rows := readRows(t, out)
	if len(rows) != 4 {
		t.Fatalf("expected header+3 rows, got %d", len(rows))
	}
	if rows[1][7] != "旧消息" || rows[3][7] != "普通聊天" {
		t.Fatalf("unexpected order/content: %v %v", rows[1][7], rows[3][7])
	}
	if rows[2][5] != "是" || rows[2][6] != "工单" {
		t.Fatalf("matched columns wrong: %v", rows[2])
	}
}

func TestExportOnlyMatchedAndRange(t *testing.T) {
	st := seedStore(t)
	out := filepath.Join(t.TempDir(), "m.xlsx")
	n, err := MessagesXLSX(st, Options{OnlyMatched: true}, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 matched row, got %d", n)
	}

	out2 := filepath.Join(t.TempDir(), "r.xlsx")
	n2, err := MessagesXLSX(st, Options{Start: time.Now().Add(-24 * time.Hour)}, out2)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 2 {
		t.Fatalf("expected 2 rows in range, got %d", n2)
	}
}
