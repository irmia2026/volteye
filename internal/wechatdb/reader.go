package wechatdb

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

func MsgTableName(wxid string) string {
	sum := md5.Sum([]byte(strings.TrimSpace(wxid)))
	return "Msg_" + hex.EncodeToString(sum[:])
}

func OpenDB(path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

type ChatTable struct {
	Name string
	Rows int64
}

func ListChatTables(db *sql.DB) ([]ChatTable, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'Msg\_%' ESCAPE '\'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []ChatTable
	for rows.Next() {
		var t ChatTable
		if err := rows.Scan(&t.Name); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	for i := range tables {
		_ = db.QueryRow(`SELECT COUNT(1) FROM ` + quoteIdent(tables[i].Name)).Scan(&tables[i].Rows)
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Rows > tables[j].Rows })
	return tables, nil
}

func tableColumns(db *sql.DB, table string) map[string]bool {
	cols := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(table) + `)`)
	if err != nil {
		return cols
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
			cols[strings.ToLower(name)] = true
		}
	}
	return cols
}

type Message struct {
	LocalID    int64
	ServerID   int64
	LocalType  int64
	SortSeq    int64
	CreateTime int64
	SenderID   int64
	SenderWxid string
	Content    string
}

func ReadLastMessages(db *sql.DB, table string, n int) ([]Message, error) {
	cols := tableColumns(db, table)
	if !cols["local_id"] || !cols["create_time"] {
		return nil, fmt.Errorf("table %s missing expected columns", table)
	}
	want := []string{"local_id", "server_id", "local_type", "sort_seq", "create_time", "real_sender_id", "message_content"}
	var sel []string
	for _, w := range want {
		if cols[w] {
			sel = append(sel, w)
		}
	}
	q := `SELECT ` + strings.Join(sel, ", ") + ` FROM ` + quoteIdent(table) +
		` ORDER BY create_time DESC, sort_seq DESC, local_id DESC LIMIT ?`
	rows, err := db.Query(q, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	for rows.Next() {
		vals := make([]any, len(sel))
		ptrs := make([]any, len(sel))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		var m Message
		for i, col := range sel {
			switch col {
			case "local_id":
				m.LocalID = toInt64(vals[i])
			case "server_id":
				m.ServerID = toInt64(vals[i])
			case "local_type":
				m.LocalType = toInt64(vals[i])
			case "sort_seq":
				m.SortSeq = toInt64(vals[i])
			case "create_time":
				m.CreateTime = toInt64(vals[i])
			case "real_sender_id":
				m.SenderID = toInt64(vals[i])
			case "message_content":
				m.Content = toText(vals[i])
			}
		}
		msgs = append(msgs, m)
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	case nil:
		return 0
	default:
		return 0
	}
}

func toText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

func Name2ID(db *sql.DB) map[int64]string {
	out := map[int64]string{}
	cols := tableColumns(db, "Name2Id")
	nameCol := ""
	for _, c := range []string{"user_name", "username", "name"} {
		if cols[c] {
			nameCol = c
			break
		}
	}
	if nameCol == "" {
		return out
	}
	rows, err := db.Query(`SELECT rowid, ` + quoteIdent(nameCol) + ` FROM Name2Id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name sql.NullString
		if err := rows.Scan(&id, &name); err == nil && name.Valid {
			out[id] = name.String
		}
	}
	return out
}

func ListChatrooms(db *sql.DB) ([]string, error) {
	cols := tableColumns(db, "SessionTable")
	if !cols["username"] {
		return nil, fmt.Errorf("SessionTable.username not found")
	}
	order := ""
	if cols["last_timestamp"] {
		order = ` ORDER BY last_timestamp DESC`
	}
	rows, err := db.Query(`SELECT username FROM SessionTable WHERE username LIKE '%@chatroom'` + order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil {
			out = append(out, u)
		}
	}
	return out, nil
}

func TypeBrief(t int64) string {
	switch t {
	case 1:
		return ""
	case 3:
		return "[图片]"
	case 34:
		return "[语音]"
	case 43, 62:
		return "[视频]"
	case 47:
		return "[表情]"
	case 48:
		return "[位置]"
	case 49:
		return "[链接/应用]"
	case 50:
		return "[通话]"
	case 10000:
		return "[系统]"
	case 244813135921:
		return "[引用]"
	case 266287972401:
		return "[聊天记录]"
	default:
		return fmt.Sprintf("[类型%d]", t)
	}
}

func Preview(s string, max int) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "..."
}
