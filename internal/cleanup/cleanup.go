package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"volteye/internal/export"
	"volteye/internal/store"
)

type Policy struct {
	RetentionDays int
	MaxDBMB       int
}

func FromSettings(st *store.Store) Policy {
	days, _ := strconv.Atoi(st.GetSetting("retention_days", "0"))
	mb, _ := strconv.Atoi(st.GetSetting("max_db_mb", "0"))
	return Policy{RetentionDays: days, MaxDBMB: mb}
}

func archivePath(dataDir, reason string) string {
	return filepath.Join(dataDir, "archive",
		fmt.Sprintf("messages_%s_%s.xlsx", time.Now().Format("20060102_150405"), reason))
}

func archive(st *store.Store, dataDir, reason string, before int64, logf func(string)) {
	path := archivePath(dataDir, reason)
	opts := export.Options{}
	if before > 0 {
		opts.End = time.Unix(before-1, 0)
	}
	count, err := export.MessagesXLSX(st, opts, path)
	if err != nil {
		logf(fmt.Sprintf("归档失败: %v", err))
		return
	}
	if count == 0 {
		os.Remove(path)
		return
	}
	logf(fmt.Sprintf("清理前已归档 %d 条: %s", count, path))
}

func RunOnce(st *store.Store, dataDir, dbPath string, logf func(string)) {
	p := FromSettings(st)
	if p.RetentionDays <= 0 && p.MaxDBMB <= 0 {
		return
	}

	if p.RetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -p.RetentionDays).Unix()
		if oldest, _ := st.OldestMessageTime(); oldest > 0 && oldest < cutoff {
			archive(st, dataDir, "retention", cutoff, logf)
			if n, err := st.DeleteMessagesBefore(cutoff); err != nil {
				logf(fmt.Sprintf("按保留期清理失败: %v", err))
			} else if n > 0 {
				logf(fmt.Sprintf("保留 %d 天策略：清理 %d 条过期消息", p.RetentionDays, n))
			}
		}
	}

	if p.MaxDBMB > 0 {
		limit := int64(p.MaxDBMB) * 1024 * 1024
		if fi, err := os.Stat(dbPath); err == nil && fi.Size() > limit {
			archive(st, dataDir, "sizecap", 0, logf)
			for i := 0; i < 50; i++ {
				n, err := st.DeleteOldestBatch(5000)
				if err != nil || n == 0 {
					break
				}
				logf(fmt.Sprintf("超出容量上限 %dMB，清理最旧 %d 条", p.MaxDBMB, n))
				st.Vacuum()
				if fi, err := os.Stat(dbPath); err != nil || fi.Size() <= limit {
					break
				}
			}
		}
	}
	st.Vacuum()
}

func StartLoop(st *store.Store, dataDir, dbPath string, interval time.Duration, logf func(string)) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			RunOnce(st, dataDir, dbPath, logf)
		}
	}()
}
