//go:build windows

package wechatdb

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

var msgDBNameRe = regexp.MustCompile(`(?i)^message(_\d+)?\.db$`)

func DefaultDBStorage() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var roots []string
	if _, err := os.Stat(filepath.Join(home, "Documents", "xwechat_files")); err == nil {
		roots = append(roots, filepath.Join(home, "Documents", "xwechat_files"))
	}
	for _, drive := range "CDEFGHIJKLMNOPQRSTUVWXYZ" {
		p := string(drive) + `:\xwechat_files`
		if _, err := os.Stat(p); err == nil {
			roots = append(roots, p)
		}
	}
	type cand struct {
		path  string
		mtime time.Time
	}
	var cands []cand
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "wxid_") {
				continue
			}
			ds := filepath.Join(root, e.Name(), "db_storage")
			msgDir := filepath.Join(ds, "message")
			files, err := os.ReadDir(msgDir)
			if err != nil {
				continue
			}
			var latest time.Time
			for _, f := range files {
				if fi, err := f.Info(); err == nil && fi.ModTime().After(latest) {
					latest = fi.ModTime()
				}
			}
			cands = append(cands, cand{ds, latest})
		}
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("no db_storage found (pass -dbstorage manually)")
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

func GetProcessExePath(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

func FindWeixinDLL(dir string) (string, error) {
	best := ""
	bestSize := int64(-1)
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.EqualFold(info.Name(), "Weixin.dll") && info.Size() > bestSize {
			best = p
			bestSize = info.Size()
		}
		return nil
	})
	if best == "" {
		return "", fmt.Errorf("Weixin.dll not found under %s", dir)
	}
	return best, nil
}
