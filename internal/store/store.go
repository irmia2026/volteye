package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Group struct {
	Wxid           string
	Name           string
	Alias          string
	Monitored      bool
	Backfill       bool
	BackfillDone   bool
	LastCreateTime int64
	LastSortSeq    int64
	LastLocalID    int64
}

type Message struct {
	GroupWxid  string
	SrcDB      string
	LocalID    int64
	ServerID   int64
	LocalType  int64
	SortSeq    int64
	CreateTime int64
	SenderWxid string
	Content    string
}

const schema = `
CREATE TABLE IF NOT EXISTS groups(
	wxid TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	alias TEXT NOT NULL DEFAULT '',
	monitored INTEGER NOT NULL DEFAULT 0,
	backfill INTEGER NOT NULL DEFAULT 0,
	backfill_done INTEGER NOT NULL DEFAULT 0,
	last_create_time INTEGER NOT NULL DEFAULT 0,
	last_sort_seq INTEGER NOT NULL DEFAULT 0,
	last_local_id INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_wxid TEXT NOT NULL,
	local_id INTEGER NOT NULL,
	server_id INTEGER NOT NULL DEFAULT 0,
	local_type INTEGER NOT NULL DEFAULT 0,
	sort_seq INTEGER NOT NULL DEFAULT 0,
	create_time INTEGER NOT NULL,
	sender_wxid TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
matched INTEGER NOT NULL DEFAULT 0,
	matched_rules TEXT NOT NULL DEFAULT '',
	scanned INTEGER NOT NULL DEFAULT 0,
	src_db TEXT NOT NULL DEFAULT '',
	UNIQUE(group_wxid, src_db, local_id)
);
CREATE TABLE IF NOT EXISTS sync_cursors(
	group_wxid TEXT NOT NULL,
	src_db TEXT NOT NULL,
	create_time INTEGER NOT NULL DEFAULT 0,
	sort_seq INTEGER NOT NULL DEFAULT 0,
	local_id INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(group_wxid, src_db)
);
CREATE INDEX IF NOT EXISTS idx_messages_group_time ON messages(group_wxid, create_time);
CREATE TABLE IF NOT EXISTS rules(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL DEFAULT '',
	keywords TEXT NOT NULL DEFAULT '',
	regex TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS settings(
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := migrateColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := migrateMessagesUnique(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate messages: %w", err)
	}
	return &Store{db: db}, nil
}

func migrateMessagesUnique(db *sql.DB) error {
	var tblSQL string
	err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='messages'").Scan(&tblSQL)
	if err != nil {
		return nil
	}
	if strings.Contains(tblSQL, "UNIQUE(group_wxid, src_db, local_id)") {
		return nil
	}
	_, err = db.Exec("ALTER TABLE messages RENAME TO messages_old")
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE messages(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_wxid TEXT NOT NULL,
	src_db TEXT NOT NULL DEFAULT '',
	local_id INTEGER NOT NULL,
	server_id INTEGER NOT NULL DEFAULT 0,
	local_type INTEGER NOT NULL DEFAULT 0,
	sort_seq INTEGER NOT NULL DEFAULT 0,
	create_time INTEGER NOT NULL,
	sender_wxid TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	matched INTEGER NOT NULL DEFAULT 0,
	matched_rules TEXT NOT NULL DEFAULT '',
	scanned INTEGER NOT NULL DEFAULT 0,
	UNIQUE(group_wxid, src_db, local_id)
)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO messages
		(id, group_wxid, src_db, local_id, server_id, local_type, sort_seq, create_time, sender_wxid, content, matched, matched_rules, scanned)
		SELECT id, group_wxid, '', local_id, server_id, local_type, sort_seq, create_time, sender_wxid, content, matched, matched_rules, scanned
		FROM messages_old`)
	if err != nil {
		return err
	}
	_, err = db.Exec("DROP TABLE messages_old")
	if err != nil {
		return err
	}
	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_group_time ON messages(group_wxid, create_time)")
	if err != nil {
		return err
	}
	_, err = db.Exec("UPDATE groups SET backfill_done=0")
	return err
}

func migrateColumns(db *sql.DB) error {
	migrate := []struct {
		table string
		col   string
		ddl   string
	}{
		{"messages", "matched_rules", `ALTER TABLE messages ADD COLUMN matched_rules TEXT NOT NULL DEFAULT ''`},
		{"messages", "scanned", `ALTER TABLE messages ADD COLUMN scanned INTEGER NOT NULL DEFAULT 0`},
		{"groups", "alias", `ALTER TABLE groups ADD COLUMN alias TEXT NOT NULL DEFAULT ''`},
		{"messages", "src_db", `ALTER TABLE messages ADD COLUMN src_db TEXT NOT NULL DEFAULT ''`},
	}
	for _, m := range migrate {
		exists := false
		rows, err := db.Query(`PRAGMA table_info(` + m.table + `)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == m.col {
				exists = true
			}
		}
		rows.Close()
		if !exists {
			if _, err := db.Exec(m.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) UpsertGroup(wxid, name string) error {
	_, err := s.db.Exec(
		`INSERT INTO groups(wxid, name) VALUES(?, ?)
		 ON CONFLICT(wxid) DO UPDATE SET name=excluded.name`,
		wxid, name,
	)
	return err
}

func (s *Store) ListGroups() ([]Group, error) {
	rows, err := s.db.Query(
		`SELECT wxid, name, alias, monitored, backfill, backfill_done, last_create_time, last_sort_seq, last_local_id
		 FROM groups ORDER BY monitored DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroups(rows)
}

func (s *Store) MonitoredGroups() ([]Group, error) {
	rows, err := s.db.Query(
		`SELECT wxid, name, alias, monitored, backfill, backfill_done, last_create_time, last_sort_seq, last_local_id
		 FROM groups WHERE monitored = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroups(rows)
}

func (g Group) DisplayName() string {
	if g.Alias != "" {
		return g.Alias
	}
	if g.Name != "" {
		return g.Name
	}
	return g.Wxid
}

func scanGroups(rows *sql.Rows) ([]Group, error) {
	var out []Group
	for rows.Next() {
		var g Group
		var mon, bf, bfd int
		if err := rows.Scan(&g.Wxid, &g.Name, &g.Alias, &mon, &bf, &bfd,
			&g.LastCreateTime, &g.LastSortSeq, &g.LastLocalID); err != nil {
			return nil, err
		}
		g.Monitored = mon != 0
		g.Backfill = bf != 0
		g.BackfillDone = bfd != 0
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) SetMonitored(wxid string, monitored bool) error {
	v := 0
	if monitored {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE groups SET monitored=? WHERE wxid=?`, v, wxid)
	return err
}

func (s *Store) SetBackfill(wxid string, backfill bool) error {
	v := 0
	if backfill {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE groups SET backfill=? WHERE wxid=?`, v, wxid)
	return err
}

func (s *Store) SaveProgress(wxid string, createTime, sortSeq, localID int64, backfillDone bool) error {
	bfd := 0
	if backfillDone {
		bfd = 1
	}
	_, err := s.db.Exec(
		`UPDATE groups SET last_create_time=?, last_sort_seq=?, last_local_id=?, backfill_done=? WHERE wxid=?`,
		createTime, sortSeq, localID, bfd, wxid,
	)
	return err
}

func (s *Store) InsertMessages(msgs []Message) (int, error) {
	if len(msgs) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO messages
		 (group_wxid, src_db, local_id, server_id, local_type, sort_seq, create_time, sender_wxid, content)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	inserted := 0
	for _, m := range msgs {
		res, err := stmt.Exec(m.GroupWxid, m.SrcDB, m.LocalID, m.ServerID, m.LocalType,
			m.SortSeq, m.CreateTime, m.SenderWxid, m.Content)
		if err != nil {
			tx.Rollback()
			return inserted, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return inserted, err
	}
	return inserted, nil
}

func (s *Store) TotalMessages() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(1) FROM messages`).Scan(&n)
	return n, err
}

func (s *Store) GroupMessageCounts() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT group_wxid, COUNT(1) FROM messages GROUP BY group_wxid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) LatestTimes() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT group_wxid, MAX(create_time) FROM messages GROUP BY group_wxid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

type Rule struct {
	ID       int64
	Name     string
	Keywords string
	Regex    string
	Enabled  bool
}

func (s *Store) ListRules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id, name, keywords, regex, enabled FROM rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.Keywords, &r.Regex, &enabled); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddRule(name, keywords, regex string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO rules(name, keywords, regex, enabled) VALUES(?, ?, ?, 1)`,
		name, keywords, regex)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteRule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM rules WHERE id=?`, id)
	return err
}

func (s *Store) SetRuleEnabled(id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE rules SET enabled=? WHERE id=?`, v, id)
	return err
}

type MessageFilter struct {
	GroupWxid   string
	Keyword     string
	OnlyMatched bool
	StartTime   int64
	EndTime     int64
	Limit       int
}

type MessageRow struct {
	ID           int64
	GroupWxid    string
	GroupName    string
	SenderWxid   string
	CreateTime   int64
	LocalType    int64
	Content      string
	Matched      bool
	MatchedRules string
}

func (s *Store) QueryMessages(f MessageFilter) ([]MessageRow, error) {
	q := `SELECT m.id, m.group_wxid, COALESCE(NULLIF(g.alias,''), NULLIF(g.name,''), ''), m.sender_wxid, m.create_time, m.local_type, m.content, m.matched, m.matched_rules
		FROM messages m LEFT JOIN groups g ON g.wxid = m.group_wxid`
	var where []string
	var args []any
	if f.GroupWxid != "" {
		where = append(where, `m.group_wxid = ?`)
		args = append(args, f.GroupWxid)
	}
	if f.Keyword != "" {
		where = append(where, `(m.content LIKE ? OR g.name LIKE ?)`)
		kw := "%" + f.Keyword + "%"
		args = append(args, kw, kw)
	}
	if f.OnlyMatched {
		where = append(where, `m.matched = 1`)
	}
	if len(where) > 0 {
		q += ` WHERE ` + where[0]
		for _, w := range where[1:] {
			q += ` AND ` + w
		}
	}
	q += ` ORDER BY m.id DESC LIMIT ?`
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MessageRow
	for rows.Next() {
		var r MessageRow
		var name sql.NullString
		var matched int
		if err := rows.Scan(&r.ID, &r.GroupWxid, &name, &r.SenderWxid, &r.CreateTime,
			&r.LocalType, &r.Content, &matched, &r.MatchedRules); err != nil {
			return nil, err
		}
		r.GroupName = name.String
		r.Matched = matched != 0
		out = append(out, r)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

type UnscannedMessage struct {
	ID      int64
	Content string
}

func (s *Store) UnscannedMessages(limit int) ([]UnscannedMessage, error) {
	rows, err := s.db.Query(`SELECT id, content FROM messages WHERE scanned = 0 ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnscannedMessage
	for rows.Next() {
		var m UnscannedMessage
		if err := rows.Scan(&m.ID, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MarkMessageScanned(id int64, ruleIDs string) error {
	matched := 0
	if ruleIDs != "" {
		matched = 1
	}
	_, err := s.db.Exec(`UPDATE messages SET scanned = 1, matched = ?, matched_rules = ? WHERE id = ?`,
		matched, ruleIDs, id)
	return err
}

func (s *Store) StreamMessages(f MessageFilter, fn func(MessageRow) error) error {
	q := `SELECT m.id, m.group_wxid, COALESCE(NULLIF(g.alias,''), NULLIF(g.name,''), ''), m.sender_wxid, m.create_time, m.local_type, m.content, m.matched, m.matched_rules
		FROM messages m LEFT JOIN groups g ON g.wxid = m.group_wxid`
	var where []string
	var args []any
	if f.GroupWxid != "" {
		where = append(where, `m.group_wxid = ?`)
		args = append(args, f.GroupWxid)
	}
	if f.Keyword != "" {
		where = append(where, `(m.content LIKE ? OR g.name LIKE ?)`)
		kw := "%" + f.Keyword + "%"
		args = append(args, kw, kw)
	}
	if f.OnlyMatched {
		where = append(where, `m.matched = 1`)
	}
	if f.StartTime > 0 {
		where = append(where, `m.create_time >= ?`)
		args = append(args, f.StartTime)
	}
	if f.EndTime > 0 {
		where = append(where, `m.create_time <= ?`)
		args = append(args, f.EndTime)
	}
	if len(where) > 0 {
		q += ` WHERE ` + where[0]
		for _, w := range where[1:] {
			q += ` AND ` + w
		}
	}
	q += ` ORDER BY m.create_time ASC, m.id ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r MessageRow
		var name sql.NullString
		var matched int
		if err := rows.Scan(&r.ID, &r.GroupWxid, &name, &r.SenderWxid, &r.CreateTime,
			&r.LocalType, &r.Content, &matched, &r.MatchedRules); err != nil {
			return err
		}
		r.GroupName = name.String
		r.Matched = matched != 0
		if err := fn(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) GetSetting(key, def string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) DeleteMessagesBefore(ts int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM messages WHERE create_time < ?`, ts)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) DeleteOldestBatch(limit int) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM messages WHERE id IN (SELECT id FROM messages ORDER BY id ASC LIMIT ?)`, limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) Vacuum() error {
	_, err := s.db.Exec(`VACUUM`)
	return err
}

func (s *Store) OldestMessageTime() (int64, error) {
	var t sql.NullInt64
	err := s.db.QueryRow(`SELECT MIN(create_time) FROM messages`).Scan(&t)
	return t.Int64, err
}

func (s *Store) DeleteGroupMessages(wxid string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM messages WHERE group_wxid=?`, wxid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) DeleteAllMessages() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM messages`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) MatchedCount() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(1) FROM messages WHERE matched=1`).Scan(&n)
	return n, err
}

func (s *Store) SetGroupAlias(wxid, alias string) error {
	_, err := s.db.Exec(`UPDATE groups SET alias=? WHERE wxid=?`, alias, wxid)
	return err
}

type ShardCursor struct {
	CreateTime int64
	SortSeq    int64
	LocalID    int64
}

func (s *Store) GetShardCursor(groupWxid, srcDB string) (ShardCursor, error) {
	var c ShardCursor
	err := s.db.QueryRow("SELECT create_time, sort_seq, local_id FROM sync_cursors WHERE group_wxid=? AND src_db=?",
		groupWxid, srcDB).Scan(&c.CreateTime, &c.SortSeq, &c.LocalID)
	if err == sql.ErrNoRows {
		return ShardCursor{}, nil
	}
	return c, err
}

func (s *Store) SaveShardCursor(groupWxid, srcDB string, c ShardCursor) error {
	_, err := s.db.Exec(
		"INSERT INTO sync_cursors(group_wxid, src_db, create_time, sort_seq, local_id) VALUES(?,?,?,?,?)"+
			" ON CONFLICT(group_wxid, src_db) DO UPDATE SET create_time=excluded.create_time, sort_seq=excluded.sort_seq, local_id=excluded.local_id",
		groupWxid, srcDB, c.CreateTime, c.SortSeq, c.LocalID)
	return err
}

func (s *Store) ResetAllCursors() error {
	_, err := s.db.Exec("DELETE FROM sync_cursors")
	return err
}
