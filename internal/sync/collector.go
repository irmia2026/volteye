package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"volteye/internal/store"
	"volteye/internal/wechatdb"
)

type Config struct {
	DBStorage  string
	Key        []byte
	Interval   time.Duration
	WorkDir    string
	Logf       func(string)
	OnPollDone func(inserted int)
}

type fileSig struct {
	size  int64
	mtime time.Time
}

type Collector struct {
	cfg     Config
	st      *store.Store
	srcDBs  []string
	sigs    map[string]fileSig
	tableDB map[string]string
	encDir  string
	decDir  string
}

func New(cfg Config, st *store.Store) *Collector {
	return &Collector{
		cfg:     cfg,
		st:      st,
		sigs:    map[string]fileSig{},
		tableDB: map[string]string{},
		encDir:  filepath.Join(cfg.WorkDir, "enc"),
		decDir:  filepath.Join(cfg.WorkDir, "dec"),
	}
}

func (c *Collector) log(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(fmt.Sprintf(format, args...))
	}
}

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
	return nil
}

func (c *Collector) refresh() {
	for _, src := range c.srcDBs {
		base := filepath.Base(src)
		encCopy := filepath.Join(c.encDir, base)
		decPath := filepath.Join(c.decDir, base)

		if sig, ok := sigOf(src); ok {
			if prev, seen := c.sigs[src]; !seen || prev != sig {
				c.sigs[src] = sig
				if err := wechatdb.CopyFile(src, encCopy); err != nil {
					c.log("copy %s failed: %v", base, err)
					continue
				}
				if _, err := wechatdb.DecryptDB(c.cfg.Key, encCopy, decPath); err != nil {
					c.log("decrypt %s failed: %v", base, err)
					continue
				}
				os.Remove(decPath + "-wal")
			}
		}

		walSrc := src + "-wal"
		if sig, ok := sigOf(walSrc); ok {
			if prev, seen := c.sigs[walSrc]; !seen || prev != sig {
				c.sigs[walSrc] = sig
				walCopy := encCopy + "-wal"
				if err := wechatdb.CopyFile(walSrc, walCopy); err != nil {
					c.log("copy wal %s failed: %v", base, err)
					continue
				}
				if _, err := wechatdb.DecryptWAL(c.cfg.Key, encCopy, walCopy, decPath+"-wal"); err != nil {
					c.log("decrypt wal %s failed: %v", base, err)
				}
			}
		} else {
			os.Remove(decPath + "-wal")
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
			if _, exists := c.tableDB[n]; !exists {
				c.tableDB[n] = decPath
			}
		}
	}
}

func (c *Collector) PollOnce() (int, error) {
	c.refresh()
	c.discover()

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
	decPath, ok := c.tableDB[table]
	if !ok {
		return 0, nil
	}
	db, err := wechatdb.OpenDB(decPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	ct, ss, lid := g.LastCreateTime, g.LastSortSeq, g.LastLocalID
	backfilling := false
	switch {
	case g.Backfill && !g.BackfillDone:
		ct, ss, lid = 0, 0, 0
		backfilling = true
	case ct == 0 && !g.BackfillDone:
		maxCT, maxSS, maxLID, err := wechatdb.LatestCursor(db, table)
		if err != nil {
			return 0, err
		}
		return 0, c.st.SaveProgress(g.Wxid, maxCT, maxSS, maxLID, true)
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
	bfd := g.BackfillDone || backfilling
	if err := c.st.SaveProgress(g.Wxid, ct, ss, lid, bfd); err != nil {
		return inserted, err
	}
	if inserted > 0 {
		label := g.Name
		if label == "" {
			label = g.Wxid
		}
		c.log("[%s] +%d message(s)", label, inserted)
	}
	return inserted, nil
}

func (c *Collector) poll() {
	n, err := c.PollOnce()
	if err != nil {
		c.log("poll error: %v", err)
		return
	}
	if c.cfg.OnPollDone != nil {
		c.cfg.OnPollDone(n)
	}
}

func (c *Collector) Run(ctx context.Context) error {
	c.poll()
	interval := c.cfg.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.poll()
		}
	}
}
