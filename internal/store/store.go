package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Group struct {
	Wxid           string
	Name           string
	Monitored      bool
	Backfill       bool
	BackfillDone   bool
	LastCreateTime int64
	LastSortSeq    int64
	LastLocalID    int64
}

type Message struct {
	GroupWxid  string
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
	UNIQUE(group_wxid, local_id)
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
	return &Store{db: db}, nil
}

func migrateColumns(db *sql.DB) error {
	want := map[string]string{
		"matched_rules": `ALTER TABLE messages ADD COLUMN matched_rules TEXT NOT NULL DEFAULT ''`,
		"scanned":       `ALTER TABLE messages ADD COLUMN scanned INTEGER NOT NULL DEFAULT 0`,
	}
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	for col, ddl := range want {
		if !existing[col] {
			if _, err := db.Exec(ddl); err != nil {
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
		`SELECT wxid, name, monitored, backfill, backfill_done, last_create_time, last_sort_seq, last_local_id
		 FROM groups ORDER BY monitored DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroups(rows)
}

func (s *Store) MonitoredGroups() ([]Group, error) {
	rows, err := s.db.Query(
		`SELECT wxid, name, monitored, backfill, backfill_done, last_create_time, last_sort_seq, last_local_id
		 FROM groups WHERE monitored = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGroups(rows)
}

func scanGroups(rows *sql.Rows) ([]Group, error) {
	var out []Group
	for rows.Next() {
		var g Group
		var mon, bf, bfd int
		if err := rows.Scan(&g.Wxid, &g.Name, &mon, &bf, &bfd,
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
		 (group_wxid, local_id, server_id, local_type, sort_seq, create_time, sender_wxid, content)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	inserted := 0
	for _, m := range msgs {
		res, err := stmt.Exec(m.GroupWxid, m.LocalID, m.ServerID, m.LocalType,
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
	q := `SELECT m.id, m.group_wxid, g.name, m.sender_wxid, m.create_time, m.local_type, m.content, m.matched, m.matched_rules
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
