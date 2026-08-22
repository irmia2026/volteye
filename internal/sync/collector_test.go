package sync

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"volteye/internal/capture"
	"volteye/internal/store"
	"volteye/internal/wechatdb"
)

func createShard(t *testing.T, path, table string, rows []struct {
	localID, createTime int64
	content             string
}) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s(
		local_id INTEGER PRIMARY KEY,
		server_id INTEGER NOT NULL DEFAULT 0,
		local_type INTEGER NOT NULL DEFAULT 1,
		sort_seq INTEGER NOT NULL DEFAULT 0,
		create_time INTEGER NOT NULL,
		real_sender_id INTEGER NOT NULL DEFAULT 0,
		message_content TEXT
	)`, table)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE Name2Id(user_name TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(fmt.Sprintf(
			`INSERT INTO %s(local_id, sort_seq, create_time, message_content) VALUES(?,?,?,?)`, table),
			r.localID, r.localID, r.createTime, r.content); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestCollector(t *testing.T, st *store.Store) *Collector {
	t.Helper()
	work := t.TempDir()
	c := New(Config{WorkDir: work, Interval: time.Second}, st)
	if err := os.MkdirAll(c.decDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.encDir, 0755); err != nil {
		t.Fatal(err)
	}
	return c
}

type row = struct {
	localID, createTime int64
	content             string
}

func TestSyncShardBackfillThenIncremental(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wxid := "group@chatroom"
	if err := st.UpsertGroup(wxid, "测试群"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMonitored(wxid, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBackfill(wxid, true); err != nil {
		t.Fatal(err)
	}

	table := wechatdb.MsgTableName(wxid)
	shard := filepath.Join(t.TempDir(), "message_0.db")
	createShard(t, shard, table, []row{{1, 1000, "历史1"}, {2, 1001, "历史2"}, {3, 1002, "历史3"}})

	c := newTestCollector(t, st)
	c.tableDBs[table] = []string{shard}

	groups, _ := st.MonitoredGroups()
	n, err := c.syncGroup(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("backfill should insert 3, got %d", n)
	}
	cur, err := st.GetShardCursor(wxid, "message_0.db")
	if err != nil {
		t.Fatal(err)
	}
	if !cur.BackfillDone {
		t.Fatal("shard backfill_done should be set after backfill")
	}

	shard2 := filepath.Join(t.TempDir(), "message_0.db")
	createShard(t, shard2, table, []row{{1, 1000, "历史1"}, {2, 1001, "历史2"}, {3, 1002, "历史3"}, {4, 1003, "新消息"}})
	c.tableDBs[table] = []string{shard2}
	n, err = c.syncGroup(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("incremental should insert 1, got %d", n)
	}
	total, _ := st.TotalMessages()
	if total != 4 {
		t.Fatalf("expected 4 total, got %d", total)
	}
}

func TestSyncShardMultiShardBackfill(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wxid := "group@chatroom"
	st.UpsertGroup(wxid, "测试群")
	st.SetMonitored(wxid, true)
	st.SetBackfill(wxid, true)

	table := wechatdb.MsgTableName(wxid)
	shardA := filepath.Join(t.TempDir(), "message_0.db")
	createShard(t, shardA, table, []row{{1, 1000, "A分片历史"}})

	c := newTestCollector(t, st)
	c.tableDBs[table] = []string{shardA}

	groups, _ := st.MonitoredGroups()
	if _, err := c.syncGroup(groups[0]); err != nil {
		t.Fatal(err)
	}

	shardB := filepath.Join(t.TempDir(), "message_3.db")
	createShard(t, shardB, table, []row{{1, 900, "B分片更早历史"}, {2, 1001, "B分片消息"}})
	c.tableDBs[table] = []string{shardA, shardB}

	n, err := c.syncGroup(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("late-appearing shard must still backfill its 2 rows, got %d", n)
	}
	cur, err := st.GetShardCursor(wxid, "message_3.db")
	if err != nil {
		t.Fatal(err)
	}
	if !cur.BackfillDone {
		t.Fatal("shard B backfill_done should be set")
	}
}

func TestSyncShardSkipToLatestWithoutBackfill(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	wxid := "group@chatroom"
	st.UpsertGroup(wxid, "测试群")
	st.SetMonitored(wxid, true)

	table := wechatdb.MsgTableName(wxid)
	shard := filepath.Join(t.TempDir(), "message_0.db")
	createShard(t, shard, table, []row{{1, 1000, "旧消息"}})

	c := newTestCollector(t, st)
	c.tableDBs[table] = []string{shard}

	groups, _ := st.MonitoredGroups()
	n, err := c.syncGroup(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("non-backfill first sync should skip history, got %d", n)
	}
	total, _ := st.TotalMessages()
	if total != 0 {
		t.Fatalf("expected 0 stored, got %d", total)
	}

	shard2 := filepath.Join(t.TempDir(), "message_0.db")
	createShard(t, shard2, table, []row{{1, 1000, "旧消息"}, {2, 1001, "新消息"}})
	c.tableDBs[table] = []string{shard2}
	n, err = c.syncGroup(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("should sync only the new message, got %d", n)
	}
}

func TestRefreshFailureDoesNotCacheSig(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ds := t.TempDir()
	msgDir := filepath.Join(ds, "message")
	if err := os.MkdirAll(msgDir, 0755); err != nil {
		t.Fatal(err)
	}
	garbage := make([]byte, 2*wechatdb.PageSize)
	for i := range garbage {
		garbage[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(msgDir, "message_0.db"), garbage, 0644); err != nil {
		t.Fatal(err)
	}

	c := New(Config{
		DBStorage: ds,
		Key:       make([]byte, wechatdb.KeySize),
		WorkDir:   t.TempDir(),
		Logf:      func(string) {},
	}, st)
	if err := c.Init(); err != nil {
		t.Fatal(err)
	}
	c.refresh()
	if len(c.sigs) != 0 {
		t.Fatalf("failed decrypt must not cache sig, got %v", c.sigs)
	}
	c.refresh()
	if len(c.sigs) != 0 {
		t.Fatalf("retry must keep sig uncached, got %v", c.sigs)
	}
}

const sampleOrderMsg = "【南方电网】【电网管理平台】尊敬的用户：您有一条新的工单，请及时处理！故障单号为：【DY2026080218290204xxx】，工单派工时间为：【2026-08-02 18:29】，故障地址：【海南省定安县】，报障描述：【客户反映一户停电。】，联系人：【女士】，联系电话：【13876655xxx】。"

func TestApplyMatchingExtractsWorkOrders(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	reg := capture.NewRegistry()
	if err := reg.Load(capture.DefaultFormats()); err != nil {
		t.Fatal(err)
	}
	c := newTestCollector(t, st)
	c.cfg.Registry = reg

	insert := func(srcDB string, localID, createTime int64) {
		if _, err := st.InsertMessages([]store.Message{
			{GroupWxid: "g@chatroom", SrcDB: srcDB, LocalID: localID, CreateTime: createTime, Content: sampleOrderMsg},
		}); err != nil {
			t.Fatal(err)
		}
	}
	insert("message_0.db", 1, 1780000000)
	c.applyMatching()

	orders, err := st.QueryWorkOrders(store.WorkOrderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d work orders, want 1", len(orders))
	}
	if orders[0].OrderNo != "DY2026080218290204xxx" || orders[0].ContactPhone != "13876655xxx" {
		t.Fatalf("unexpected order: %+v", orders[0])
	}
	if orders[0].GroupWxid != "g@chatroom" || orders[0].SrcDB != "message_0.db" || orders[0].LocalID != 1 {
		t.Fatalf("trace fields lost: %+v", orders[0])
	}

	// 同一单号再次落库(消息主键不同) -> 解析成功但 work_orders 去重
	insert("message_1.db", 9, 1780000100)
	c.applyMatching()
	if n, _ := st.WorkOrderCount(); n != 1 {
		t.Fatalf("duplicate order_no must be ignored, got %d", n)
	}
	if n, _ := st.ParseErrorCount(); n != 0 {
		t.Fatalf("unexpected parse errors: %d", n)
	}
}

func TestApplyMatchingRecordsParseError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	reg := capture.NewRegistry()
	if err := reg.Load(capture.DefaultFormats()); err != nil {
		t.Fatal(err)
	}
	c := newTestCollector(t, st)
	c.cfg.Registry = reg

	// 签名命中但工单号为空 -> 解析失败必须记录,不能静默丢单
	broken := "【南方电网】尊敬的用户：您有一条[新]工单到达，工作单号为：[]，地址为：[海口市]"
	if _, err := st.InsertMessages([]store.Message{
		{GroupWxid: "g@chatroom", SrcDB: "message_0.db", LocalID: 1, CreateTime: 1780000000, Content: broken},
	}); err != nil {
		t.Fatal(err)
	}
	c.applyMatching()
	errs, err := st.ListParseErrors(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 {
		t.Fatalf("got %d parse errors, want 1", len(errs))
	}
	if errs[0].MessageID == 0 || errs[0].Reason == "" {
		t.Fatalf("parse error not recorded properly: %+v", errs[0])
	}
}
