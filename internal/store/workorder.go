package store

import (
	"strconv"

	"volteye/internal/capture"
)

// ---------------------------------------------------------------------------
// formats: data-driven Extractor definitions
// ---------------------------------------------------------------------------

// formatsSeedVersion bumps whenever DefaultFormats changes; existing databases
// get their built-in format rows updated in place on next open.
// v2: narrowed csg-b signature to its bracket style, added category alias,
// added csg-c (book-title-bracket work-order variant).
// v3: added contact_way (联系方式) alias to csg-b/csg-c.
// v4: added csg-d (full-width-parenthesis work-order variant).
// v5: signatures anchor on the bracketed key only (covers 【海南电网】 and
// prefix-less forwards); added csg-e (报障单, KF order numbers).
const formatsSeedVersion = 5

// SeedFormats upserts the built-in formats by name. Format definitions are
// owned by the seed for built-ins: upgrading overwrites local edits to them,
// while user-created formats (other names) are never touched.
func (s *Store) SeedFormats() error {
	v, _ := strconv.Atoi(s.GetSetting("formats_seed_version", "0"))
	if v >= formatsSeedVersion {
		return nil
	}
	for _, f := range capture.DefaultFormats() {
		enabled := 0
		if f.Enabled {
			enabled = 1
		}
		res, err := s.db.Exec(
			`UPDATE formats SET kind=?, signature=?, open_b=?, close_b=?, aliases=?, category_key=? WHERE name=?`,
			orDefault(f.Kind, "bracketkv"), f.Signature, f.OpenB, f.CloseB, f.Aliases, f.CategoryKey, f.Name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := s.db.Exec(
				`INSERT OR IGNORE INTO formats(name, kind, signature, open_b, close_b, aliases, category_key, enabled)
				 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
				f.Name, orDefault(f.Kind, "bracketkv"), f.Signature, f.OpenB, f.CloseB, f.Aliases, f.CategoryKey, enabled); err != nil {
				return err
			}
		}
	}
	return s.SetSetting("formats_seed_version", strconv.Itoa(formatsSeedVersion))
}

func (s *Store) ListFormats() ([]capture.FormatConfig, error) {
	rows, err := s.db.Query(
		`SELECT id, name, kind, signature, open_b, close_b, aliases, category_key, enabled FROM formats ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []capture.FormatConfig
	for rows.Next() {
		var f capture.FormatConfig
		var enabled int
		if err := rows.Scan(&f.ID, &f.Name, &f.Kind, &f.Signature, &f.OpenB, &f.CloseB,
			&f.Aliases, &f.CategoryKey, &enabled); err != nil {
			return nil, err
		}
		f.Enabled = enabled != 0
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) AddFormat(f capture.FormatConfig) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO formats(name, kind, signature, open_b, close_b, aliases, category_key, enabled)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Name, orDefault(f.Kind, "bracketkv"), f.Signature, orDefault(f.OpenB, "【"),
		orDefault(f.CloseB, "】"), f.Aliases, f.CategoryKey, 1)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateFormat(f capture.FormatConfig) error {
	_, err := s.db.Exec(
		`UPDATE formats SET name=?, signature=?, open_b=?, close_b=?, aliases=?, category_key=? WHERE id=?`,
		f.Name, f.Signature, f.OpenB, f.CloseB, f.Aliases, f.CategoryKey, f.ID)
	return err
}

func (s *Store) DeleteFormat(id int64) error {
	_, err := s.db.Exec(`DELETE FROM formats WHERE id=?`, id)
	return err
}

func (s *Store) SetFormatEnabled(id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE formats SET enabled=? WHERE id=?`, v, id)
	return err
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ---------------------------------------------------------------------------
// work_orders
// ---------------------------------------------------------------------------

// InsertWorkOrders stores parsed orders; order_no is unique so re-forwarded
// duplicates are ignored. It returns how many rows were actually inserted.
func (s *Store) InsertWorkOrders(orders []capture.WorkOrder) (int, error) {
	if len(orders) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO work_orders
		 (order_no, format, priority, category, dispatch_time, address, description,
		  contact_name, contact_way, contact_phone, user_no, user_name,
		  group_wxid, sender_wxid, src_db, msg_local_id, create_time, raw)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	inserted := 0
	for _, o := range orders {
		res, err := stmt.Exec(o.OrderNo, o.Format, o.Priority, o.Category, o.DispatchTime,
			o.Address, o.Description, o.ContactName, o.ContactWay, o.ContactPhone, o.UserNo, o.UserName,
			o.GroupWxid, o.SenderWxid, o.SrcDB, o.LocalID, o.CreateTime, o.Raw)
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

type WorkOrderFilter struct {
	Keyword   string
	StartTime int64
	EndTime   int64
	Limit     int
}

const workOrderCols = `id, order_no, format, priority, category, dispatch_time, address, description,
	contact_name, contact_way, contact_phone, user_no, user_name, group_wxid, sender_wxid, src_db, msg_local_id, create_time, raw`

func scanWorkOrder(row interface{ Scan(...any) error }) (capture.WorkOrder, error) {
	var o capture.WorkOrder
	var id int64
	err := row.Scan(&id, &o.OrderNo, &o.Format, &o.Priority, &o.Category, &o.DispatchTime,
		&o.Address, &o.Description, &o.ContactName, &o.ContactWay, &o.ContactPhone, &o.UserNo, &o.UserName,
		&o.GroupWxid, &o.SenderWxid, &o.SrcDB, &o.LocalID, &o.CreateTime, &o.Raw)
	return o, err
}

func workOrderWhere(f WorkOrderFilter) (string, []any) {
	var where []string
	var args []any
	if f.Keyword != "" {
		where = append(where, `(order_no LIKE ? OR address LIKE ? OR description LIKE ? OR contact_phone LIKE ?)`)
		kw := "%" + f.Keyword + "%"
		args = append(args, kw, kw, kw, kw)
	}
	if f.StartTime > 0 {
		where = append(where, `dispatch_time >= ?`)
		args = append(args, f.StartTime)
	}
	if f.EndTime > 0 {
		where = append(where, `dispatch_time <= ?`)
		args = append(args, f.EndTime)
	}
	q := ""
	if len(where) > 0 {
		q = ` WHERE ` + where[0]
		for _, w := range where[1:] {
			q += ` AND ` + w
		}
	}
	return q, args
}

func (s *Store) QueryWorkOrders(f WorkOrderFilter) ([]capture.WorkOrder, error) {
	q, args := workOrderWhere(f)
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT `+workOrderCols+` FROM work_orders`+q+` ORDER BY id DESC LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []capture.WorkOrder
	for rows.Next() {
		o, err := scanWorkOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// StreamWorkOrders walks matching orders ascending without a limit, for export.
func (s *Store) StreamWorkOrders(f WorkOrderFilter, fn func(capture.WorkOrder) error) error {
	q, args := workOrderWhere(f)
	rows, err := s.db.Query(`SELECT `+workOrderCols+` FROM work_orders`+q+` ORDER BY dispatch_time ASC, id ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		o, err := scanWorkOrder(rows)
		if err != nil {
			return err
		}
		if err := fn(o); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) WorkOrderCount() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(1) FROM work_orders`).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// parse_errors: signature matched but no order could be parsed
// ---------------------------------------------------------------------------

func (s *Store) InsertParseError(messageID int64, format, reason, content string) error {
	_, err := s.db.Exec(
		`INSERT INTO parse_errors(message_id, format, reason, content, create_time)
		 VALUES(?, ?, ?, ?, strftime('%s','now'))`,
		messageID, format, reason, content)
	return err
}

type ParseError struct {
	ID        int64
	MessageID int64
	Format    string
	Reason    string
	Content   string
	CreatedAt int64
}

func (s *Store) ListParseErrors(limit int) ([]ParseError, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT id, message_id, format, reason, content, create_time FROM parse_errors ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParseError
	for rows.Next() {
		var e ParseError
		if err := rows.Scan(&e.ID, &e.MessageID, &e.Format, &e.Reason, &e.Content, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ParseErrorCount() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(1) FROM parse_errors`).Scan(&n)
	return n, err
}

func (s *Store) ClearParseErrors() error {
	_, err := s.db.Exec(`DELETE FROM parse_errors`)
	return err
}
