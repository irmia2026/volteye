package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"volteye/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "volteye.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewService(st, nil, nil, dir, nil), st
}

func TestClearAllMessages(t *testing.T) {
	svc, st := newTestService(t)
	if _, err := st.InsertMessages([]store.Message{
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 1, CreateTime: 1000, Content: "甲"},
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 2, CreateTime: 1001, Content: "乙"},
	}); err != nil {
		t.Fatal(err)
	}
	n, path, err := svc.ClearAllMessages()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deleted, got %d", n)
	}
	if path == "" {
		t.Fatal("expected archive path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	total, _ := st.TotalMessages()
	if total != 0 {
		t.Fatalf("expected empty store, got %d", total)
	}
}

func TestClearAllMessagesAbortsWhenArchiveFails(t *testing.T) {
	svc, st := newTestService(t)
	if _, err := st.InsertMessages([]store.Message{
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 1, CreateTime: 1000, Content: "甲"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc.DataDir, "archive"), []byte("block"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.ClearAllMessages()
	if err == nil {
		t.Fatal("expected archive failure")
	}
	total, _ := st.TotalMessages()
	if total != 1 {
		t.Fatalf("messages must survive failed archive, got %d", total)
	}
}

func TestClearAllMessagesEmptyStore(t *testing.T) {
	svc, _ := newTestService(t)
	n, path, err := svc.ClearAllMessages()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || path != "" {
		t.Fatalf("expected (0, \"\"), got (%d, %s)", n, path)
	}
}

func TestAddRuleValidation(t *testing.T) {
	svc, st := newTestService(t)
	if err := svc.AddRule("", "kw", ""); err == nil {
		t.Fatal("empty name should fail")
	}
	if err := svc.AddRule("r", "", ""); err == nil {
		t.Fatal("empty keywords and regex should fail")
	}
	if err := svc.AddRule("r", "", "(unclosed"); err == nil {
		t.Fatal("bad regex should fail")
	}
	if err := svc.AddRule("工单", "工单,报修", ""); err != nil {
		t.Fatal(err)
	}
	rules, _ := st.ListRules()
	if len(rules) != 1 || rules[0].Name != "工单" {
		t.Fatalf("rule not persisted: %+v", rules)
	}
	if hits := svc.Engine.Match("收到新工单请处理"); len(hits) != 1 {
		t.Fatalf("engine should match after AddRule, got %v", hits)
	}
}

func TestSetRuleEnabledSyncsEngine(t *testing.T) {
	svc, st := newTestService(t)
	id, err := st.AddRule("工单", "工单", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReloadRules(); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRuleEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	if hits := svc.Engine.Match("新工单"); len(hits) != 0 {
		t.Fatalf("disabled rule should not match, got %v", hits)
	}
}

func TestRescanAll(t *testing.T) {
	svc, st := newTestService(t)
	st.InsertMessages([]store.Message{
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 1, CreateTime: 1, Content: "a"},
	})
	st.MarkMessageScanned(1, "1")
	n, err := svc.RescanAll()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 reset, got %d", n)
	}
	pending, _ := st.UnscannedMessages(10)
	if len(pending) != 1 {
		t.Fatalf("expected 1 unscanned, got %d", len(pending))
	}
}

func TestPollIntervalFallback(t *testing.T) {
	svc, _ := newTestService(t)
	if d := svc.PollInterval(); d != 3*time.Second {
		t.Fatalf("default interval should be 3s, got %s", d)
	}
	if err := svc.SetPollInterval(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if d := svc.PollInterval(); d != 5*time.Second {
		t.Fatalf("expected 5s, got %s", d)
	}
}
