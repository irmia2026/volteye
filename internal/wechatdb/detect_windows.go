//go:build windows

package wechatdb

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var msgDBNameRe = regexp.MustCompile(`(?i)^message(_\d+)?\.db$`)

func DefaultDBStorage() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := filepath.Join(home, "Documents", "xwechat_files")
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %v (pass -dbstorage manually)", base, err)
	}
	type cand struct {
		path  string
		mtime time.Time
	}
	var cands []cand
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ds := filepath.Join(base, e.Name(), "db_storage")
		if st, err := os.Stat(ds); err == nil && st.IsDir() {
			cands = append(cands, cand{ds, st.ModTime()})
		}
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("no db_storage found under %s (pass -dbstorage manually)", base)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mtime.After(cands[j].mtime) })
	return cands[0].path, nil
}

func ListMessageDBs(dbStorage string) ([]string, error) {
	msgDir := filepath.Join(dbStorage, "message")
	entries, err := os.ReadDir(msgDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v", msgDir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if msgDBNameRe.MatchString(e.Name()) {
			out = append(out, filepath.Join(msgDir, e.Name()))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no message_*.db under %s", msgDir)
	}
	return out, nil
}

func FindVerifyPage(dbStorage string) (string, []byte, error) {
	candidates := []string{
		filepath.Join(dbStorage, "session", "session.db"),
		filepath.Join(dbStorage, "session.db"),
	}
	if dbs, err := ListMessageDBs(dbStorage); err == nil {
		candidates = append(candidates, dbs...)
	}
	_ = filepath.Walk(dbStorage, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(p) == ".db" {
			candidates = append(candidates, p)
		}
		return nil
	})
	for _, p := range candidates {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		buf := make([]byte, PageSize)
		n, _ := f.Read(buf)
		f.Close()
		if n >= PageSize {
			return p, buf, nil
		}
	}
	return "", nil, fmt.Errorf("no readable db file found under %s", dbStorage)
}

func FindSessionDB(dbStorage string) string {
	for _, p := range []string{
		filepath.Join(dbStorage, "session", "session.db"),
		filepath.Join(dbStorage, "session.db"),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
