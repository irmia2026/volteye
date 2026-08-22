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
	"golang.org/x/sys/windows/registry"
)

var msgDBNameRe = regexp.MustCompile(`(?i)^message(_\d+)?\.db$`)

// filesRootFromRegistry reads the custom file-storage location the user may
// have set in WeChat settings. Best-effort: key names differ between WeChat
// versions, and the value may point at xwechat_files itself or its parent.
func filesRootFromRegistry() []string {
	var out []string
	for _, probe := range []struct{ path, val string }{
		{`Software\Tencent\Weixin`, "FileSavePath"},
		{`Software\Tencent\WeChat`, "FileSavePath"},
	} {
		key, err := registry.OpenKey(registry.CURRENT_USER, probe.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue(probe.val)
		key.Close()
		if err != nil {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "MyDocument") {
			continue
		}
		out = append(out, v)
	}
	return out
}

// hasMessageDBs reports whether root looks like an xwechat_files directory.
func hasMessageDBs(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "wxid_") {
			continue
		}
		msgDir := filepath.Join(root, e.Name(), "db_storage", "message")
		if files, err := os.ReadDir(msgDir); err == nil {
			for _, f := range files {
				if !f.IsDir() && msgDBNameRe.MatchString(f.Name()) {
					return true
				}
			}
		}
	}
	return false
}

// filesRootFromXwechatINI reads WeChat 4.x's own record of the files
// location: %APPDATA%\Tencent\xwechat\config\<hash>.ini holds the parent
// directory of xwechat_files (e.g. "D:\"). This is the pointer 4.x actually
// maintains; the registry only keeps OldFileSavePath from migrations.
func filesRootFromXwechatINI() []string {
	cfgDir := filepath.Join(os.Getenv("APPDATA"), "Tencent", "xwechat", "config")
	entries, err := os.ReadDir(cfgDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".ini") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cfgDir, e.Name()))
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(data))
		if v == "" || !strings.Contains(v, `:\`) {
			continue
		}
		out = append(out, v)
	}
	return out
}

func DefaultDBStorage() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		if hasMessageDBs(p) {
			roots = append(roots, p)
		}
	}
	// registry (3.x / migrated installs) and the 4.x config ini may point at
	// xwechat_files itself or its parent directory
	for _, v := range append(filesRootFromRegistry(), filesRootFromXwechatINI()...) {
		add(v)
		add(filepath.Join(v, "xwechat_files"))
		add(filepath.Join(v, "xwechat files"))
	}
	// default locations; some installs name the folder "xwechat files" (with a space)
	for _, name := range []string{"xwechat_files", "xwechat files"} {
		add(filepath.Join(home, "Documents", name))
		add(filepath.Join(home, name))
		for _, drive := range "CDEFGHIJKLMNOPQRSTUVWXYZ" {
			add(string(drive) + `:\` + name)
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
