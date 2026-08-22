package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"volteye/internal/capture"
	"volteye/internal/store"
	"volteye/internal/wechatdb"
)

type Matcher interface {
	Match(content string) []int64
}

type Config struct {
	DBStorage    string
	Key          []byte
	Interval     time.Duration
	GroupRefresh time.Duration
	WorkDir      string
	Logf         func(string)
	OnPollDone   func(inserted int)
	Matcher      Matcher
	Registry     *capture.Registry
}

type fileSig struct {
	size  int64
	mtime time.Time
}

type Collector struct {
	cfg        Config
	interval   atomic.Int64
	st         *store.Store
	srcDBs     []string
	sigs       map[string]fileSig
	tableDBs   map[string][]string
	encDir     string
	decDir     string
	lastGroups time.Time
}

func New(cfg Config, st *store.Store) *Collector {
	c := &Collector{
		cfg:      cfg,
		st:       st,
		sigs:     map[string]fileSig{},
		tableDBs: map[string][]string{},
		encDir:   filepath.Join(cfg.WorkDir, "enc"),
		decDir:   filepath.Join(cfg.WorkDir, "dec"),
	}
	c.SetInterval(cfg.Interval)
	if cfg.GroupRefresh <= 0 {
		c.cfg.GroupRefresh = 10 * time.Minute
	}
	return c
}

func (c *Collector) SetInterval(d time.Duration) {
	if d < time.Second {
		d = time.Second
	}
	c.interval.Store(int64(d))
}

func (c *Collector) Interval() time.Duration {
	return time.Duration(c.interval.Load())
}

func (c *Collector) log(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(fmt.Sprintf(format, args...))
	}
}

func walSigKey(src string) string { return src + "-wal" }

func sigOf(path string) (fileSig, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return fileSig{}, false
	}
	return fileSig{st.Size(), st.ModTime()}, true
}

func (c *Collector) Init() error {
	dbs, err := wechatdb.ListMessageDBs(c.cfg.DBStorage)
	if err != nil {
		return err
	}
	c.srcDBs = dbs
	for _, d := range []string{c.encDir, c.decDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	// enc/ only holds transient copies during decrypt; drop leftovers from
	// previous runs so disk usage stays at one decrypted mirror per shard.
	if entries, err := os.ReadDir(c.encDir); err == nil {
		for _, e := range entries {
			os.Remove(filepath.Join(c.encDir, e.Name()))
		}
	}
	return nil
}

func (c *Collector) refresh() {
	for _, src := range c.srcDBs {
		base := filepath.Base(src)
		encCopy := filepath.Join(c.encDir, base)
		decPath := filepath.Join(c.decDir, base)

		if sig, ok := sigOf(src); ok {
			if prev, seen := c.sigs[src]; !seen || prev != sig {
				if err := wechatdb.CopyFile(src, encCopy); err != nil {
					c.log("copy %s failed: %v", base, err)
					continue
				}
				// The encrypted copy is only an intermediate for decrypt;
				// remove it right away so no full enc mirror persists.
				_, err := wechatdb.DecryptDB(c.cfg.Key, encCopy, decPath)
				os.Remove(encCopy)
				if err != nil {
					c.log("decrypt %s failed: %v", base, err)
					continue
				}
				c.sigs[src] = sig
				os.Remove(decPath + "-wal")
				os.Remove(encCopy + "-wal")
				delete(c.sigs, walSigKey(src))
			}
		}

		walSrc := walSigKey(src)
		if sig, ok := sigOf(walSrc); ok {
			if prev, seen := c.sigs[walSrc]; !seen || prev != sig {
				salt, err := wechatdb.ReadSalt(src)
				if err != nil {
					c.log("read salt %s failed: %v", base, err)
					continue
				}
				walCopy := encCopy + "-wal"
				if err := wechatdb.CopyFile(walSrc, walCopy); err != nil {
					c.log("copy wal %s failed: %v", base, err)
					continue
				}
				_, err = wechatdb.DecryptWAL(c.cfg.Key, salt, walCopy, decPath+"-wal")
				os.Remove(walCopy)
				if err != nil {
					c.log("decrypt wal %s failed: %v", base, err)
					continue
				}
				c.sigs[walSrc] = sig
			}
		} else {
			os.Remove(decPath + "-wal")
			os.Remove(encCopy + "-wal")
			delete(c.sigs, walSrc)
		}
	}
}

func (c *Collector) discover() {
	for _, src := range c.srcDBs {
		decPath := filepath.Join(c.decDir, filepath.Base(src))
		if _, err := os.Stat(decPath); err != nil {
			continue
		}
		db, err := wechatdb.OpenDB(decPath)
		if err != nil {
			continue
		}
		names, err := wechatdb.ListChatTableNames(db)
		db.Close()
		if err != nil {
			continue
		}
		for _, n := range names {
			found := false
			for _, existing := range c.tableDBs[n] {
				if existing == decPath {
					found = true
					break
				}
			}
			if !found {
				c.tableDBs[n] = append(c.tableDBs[n], decPath)
			}
		}
	}
}

func (c *Collector) RefreshGroups() (int, int, error) {
	return DiscoverGroups(c.cfg.DBStorage, c.cfg.Key, c.encDir, c.decDir, c.st)
}

func (c *Collector) PollOnce() (int, error) {
	c.refresh()
	c.discover()

	if time.Since(c.lastGroups) >= c.cfg.GroupRefresh {
		if total, named, err := c.RefreshGroups(); err != nil {
			c.log("refresh groups failed: %v", err)
		} else {
			c.lastGroups = time.Now()
			c.log("群列表已刷新: %d 个群（%d 个有名称）", total, named)
		}
	}

	groups, err := c.st.MonitoredGroups()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, g := range groups {
		n, err := c.syncGroup(g)
		if err != nil {
			c.log("sync %s failed: %v", g.Wxid, err)
			continue
		}
		total += n
	}
	return total, nil
}

func (c *Collector) syncGroup(g store.Group) (int, error) {
	table := wechatdb.MsgTableName(g.Wxid)
	decPaths, ok := c.tableDBs[table]
	if !ok {
		return 0, nil
	}
	totalInserted := 0
	for _, decPath := range decPaths {
		n, err := c.syncShard(g, table, decPath)
		if err != nil {
			return totalInserted, err
		}
		totalInserted += n
	}
	if totalInserted > 0 {
		label := g.Name
		if label == "" {
			label = g.Wxid
		}
		c.log("[%s] +%d message(s)", label, totalInserted)
	}
	return totalInserted, nil
}

func (c *Collector) syncShard(g store.Group, table, decPath string) (int, error) {
	srcDB := filepath.Base(decPath)
	db, err := wechatdb.OpenDB(decPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	cur, err := c.st.GetShardCursor(g.Wxid, srcDB)
	if err != nil {
		return 0, err
	}

	ct, ss, lid := cur.CreateTime, cur.SortSeq, cur.LocalID
	backfilling := false
	switch {
	case g.Backfill && !cur.BackfillDone:
		ct, ss, lid = 0, 0, 0
		backfilling = true
	case ct == 0 && ss == 0 && lid == 0:
		maxCT, maxSS, maxLID, err := wechatdb.LatestCursor(db, table)
		if err != nil {
			return 0, err
		}
		return 0, c.st.SaveShardCursor(g.Wxid, srcDB, store.ShardCursor{CreateTime: maxCT, SortSeq: maxSS, LocalID: maxLID})
	}

	nameMap := wechatdb.Name2ID(db)
	inserted := 0
	for {
		msgs, err := wechatdb.ReadMessagesSince(db, table, ct, ss, lid, 500)
		if err != nil {
			return inserted, err
		}
		if len(msgs) == 0 {
			break
		}
		batch := make([]store.Message, 0, len(msgs))
		for _, m := range msgs {
			sender := m.SenderWxid
			if sender == "" {
				sender = nameMap[m.SenderID]
			}
			content := wechatdb.StripSenderPrefix(m.Content, sender)
			batch = append(batch, store.Message{
				GroupWxid:  g.Wxid,
				SrcDB:      srcDB,
				LocalID:    m.LocalID,
				ServerID:   m.ServerID,
				LocalType:  m.LocalType,
				SortSeq:    m.SortSeq,
				CreateTime: m.CreateTime,
				SenderWxid: sender,
				Content:    content,
			})
			ct, ss, lid = m.CreateTime, m.SortSeq, m.LocalID
		}
		n, err := c.st.InsertMessages(batch)
		if err != nil {
			return inserted, err
		}
		inserted += n
		if len(msgs) < 500 {
			break
		}
	}
	if err := c.st.SaveShardCursor(g.Wxid, srcDB, store.ShardCursor{CreateTime: ct, SortSeq: ss, LocalID: lid}); err != nil {
		return inserted, err
	}
	if backfilling {
		if err := c.st.MarkShardBackfillDone(g.Wxid, srcDB); err != nil {
			return inserted, err
		}
		if err := c.st.SaveProgress(g.Wxid, ct, ss, lid, true); err != nil {
			return inserted, err
		}
	}
	return inserted, nil
}

func (c *Collector) applyMatching() {
	if c.cfg.Matcher == nil && c.cfg.Registry == nil {
		return
	}
	for {
		rows, err := c.st.UnscannedMessages(500)
		if err != nil {
			c.log("match scan failed: %v", err)
			return
		}
		if len(rows) == 0 {
			return
		}
		for _, m := range rows {
			var ids []string
			if c.cfg.Matcher != nil {
				hits := c.cfg.Matcher.Match(m.Content)
				for _, id := range hits {
					ids = append(ids, strconv.FormatInt(id, 10))
				}
			}
			if c.cfg.Registry != nil {
				c.extractOrders(m)
			}
			if err := c.st.MarkMessageScanned(m.ID, strings.Join(ids, ",")); err != nil {
				c.log("mark message %d failed: %v", m.ID, err)
			}
		}
		if len(rows) < 500 {
			return
		}
	}
}

// extractOrders runs the format registry over one message. Signature hits
// that fail to parse are recorded in parse_errors so format drift surfaces
// instead of silently dropping orders.
func (c *Collector) extractOrders(m store.UnscannedMessage) {
	orders, err := c.cfg.Registry.Extract(capture.Message{
		ID:         m.ID,
		GroupWxid:  m.GroupWxid,
		SenderWxid: m.SenderWxid,
		SrcDB:      m.SrcDB,
		LocalID:    m.LocalID,
		CreateTime: m.CreateTime,
		Content:    m.Content,
	})
	if err != nil {
		if e := c.st.InsertParseError(m.ID, "", err.Error(), m.Content); e != nil {
			c.log("record parse error failed: %v", e)
		}
		c.log("工单解析失败(msg %d): %v", m.ID, err)
	}
	if len(orders) == 0 {
		return
	}
	n, err := c.st.InsertWorkOrders(orders)
	if err != nil {
		c.log("insert work orders failed: %v", err)
		return
	}
	if n > 0 {
		c.log("新工单 +%d [%s]", n, orders[0].OrderNo)
	}
}

func (c *Collector) poll() {
	n, err := c.PollOnce()
	if err != nil {
		c.log("poll error: %v", err)
		return
	}
	c.applyMatching()
	if c.cfg.OnPollDone != nil {
		c.cfg.OnPollDone(n)
	}
}

func (c *Collector) Run(ctx context.Context) error {
	c.poll()
	for {
		timer := time.NewTimer(c.Interval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			c.poll()
		}
	}
}
