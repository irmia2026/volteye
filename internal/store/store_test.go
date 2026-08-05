package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestShardCursorRoundtrip(t *testing.T) {
	st := openTestStore(t)

	cur, err := st.GetShardCursor("g@chatroom", "message_0.db")
	if err != nil {
		t.Fatal(err)
	}
	if cur.CreateTime != 0 || cur.BackfillDone {
		t.Fatalf("expected empty cursor, got %+v", cur)
	}

	if err := st.SaveShardCursor("g@chatroom", "message_0.db", ShardCursor{CreateTime: 100, SortSeq: 5, LocalID: 42}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkShardBackfillDone("g@chatroom", "message_0.db"); err != nil {
		t.Fatal(err)
	}
	cur, err = st.GetShardCursor("g@chatroom", "message_0.db")
	if err != nil {
		t.Fatal(err)
	}
	if cur.CreateTime != 100 || cur.SortSeq != 5 || cur.LocalID != 42 || !cur.BackfillDone {
		t.Fatalf("unexpected cursor %+v", cur)
	}

	if err := st.SaveShardCursor("g@chatroom", "message_0.db", ShardCursor{CreateTime: 200, SortSeq: 6, LocalID: 43}); err != nil {
		t.Fatal(err)
	}
	cur, err = st.GetShardCursor("g@chatroom", "message_0.db")
	if err != nil {
		t.Fatal(err)
	}
	if cur.CreateTime != 200 || !cur.BackfillDone {
		t.Fatalf("SaveShardCursor must preserve backfill_done, got %+v", cur)
	}
}

func TestShardBackfillMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE groups(
		wxid TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		monitored INTEGER NOT NULL DEFAULT 0,
		backfill INTEGER NOT NULL DEFAULT 0,
		backfill_done INTEGER NOT NULL DEFAULT 0,
		last_create_time INTEGER NOT NULL DEFAULT 0,
		last_sort_seq INTEGER NOT NULL DEFAULT 0,
		last_local_id INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE sync_cursors(
		group_wxid TEXT NOT NULL,
		src_db TEXT NOT NULL,
		create_time INTEGER NOT NULL DEFAULT 0,
		sort_seq INTEGER NOT NULL DEFAULT 0,
		local_id INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(group_wxid, src_db)
	);
	INSERT INTO groups(wxid, backfill_done) VALUES('done@chatroom', 1), ('pending@chatroom', 0);
	INSERT INTO sync_cursors(group_wxid, src_db, create_time) VALUES
		('done@chatroom', 'message_0.db', 111),
		('pending@chatroom', 'message_0.db', 222);`)
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	raw.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cur, err := st.GetShardCursor("done@chatroom", "message_0.db")
	if err != nil {
		t.Fatal(err)
	}
	if cur.CreateTime != 111 || !cur.BackfillDone {
		t.Fatalf("legacy group backfill_done=1 must migrate to shard flag, got %+v", cur)
	}
	cur, err = st.GetShardCursor("pending@chatroom", "message_0.db")
	if err != nil {
		t.Fatal(err)
	}
	if cur.CreateTime != 222 || cur.BackfillDone {
		t.Fatalf("legacy group backfill_done=0 must stay pending, got %+v", cur)
	}
}

func TestResetScanned(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.InsertMessages([]Message{
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 1, CreateTime: 1, Content: "a"},
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 2, CreateTime: 2, Content: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkMessageScanned(1, "1"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkMessageScanned(2, ""); err != nil {
		t.Fatal(err)
	}
	pending, err := st.UnscannedMessages(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 unscanned, got %d", len(pending))
	}
	n, err := st.ResetScanned()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 reset, got %d", n)
	}
	pending, err = st.UnscannedMessages(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 unscanned after reset, got %d", len(pending))
	}
}
