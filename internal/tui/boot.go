package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/cleanup"
	"volteye/internal/extract"
	"volteye/internal/store"
	"volteye/internal/sync"
	"volteye/internal/wechatdb"
)

func nilContext() context.Context { return context.Background() }

func DefaultBoot(cfg AppConfig, send func(tea.Msg)) (*store.Store, *sync.Collector, error) {
	step := func(s string) { send(bootStepMsg{text: s}) }

	ds := cfg.DBStorage
	if ds == "" {
		var err error
		ds, err = wechatdb.DefaultDBStorage()
		if err != nil {
			return nil, nil, err
		}
	}
	step("数据目录: " + ds)

	_, page1, err := wechatdb.FindVerifyPage(ds)
	if err != nil {
		return nil, nil, err
	}

	var rawKey []byte
	if cfg.KeyHex != "" {
		rawKey, err = wechatdb.ParseKeyHex(cfg.KeyHex)
		if err != nil {
			return nil, nil, err
		}
		if !wechatdb.VerifyPage1Key(rawKey, page1) {
			return nil, nil, fmt.Errorf("手动提供的密钥校验失败")
		}
		step("手动密钥校验通过")
	} else {
		pid, name, err := wechatdb.FindWeChatPID()
		if err != nil {
			return nil, nil, err
		}
		step(fmt.Sprintf("%s PID=%d", name, pid))
		var internalKeys [][]byte
		if exePath, err := wechatdb.GetProcessExePath(pid); err == nil {
			if dll, err := wechatdb.FindWeixinDLL(filepath.Dir(exePath)); err == nil {
				internalKeys, _ = wechatdb.ExtractInternalKeys(dll)
			}
		}
		step(fmt.Sprintf("DLL 辅助密钥候选: %d 个", len(internalKeys)))
		step("扫描进程内存提取数据库密钥 ...")
		t0 := time.Now()
		rawKey, err = wechatdb.RecoverKey(pid, page1, internalKeys, func(s string) { step(s) })
		if err != nil {
			return nil, nil, err
		}
		step(fmt.Sprintf("密钥提取成功，耗时 %s", time.Since(t0).Round(time.Millisecond)))
	}

	workDir := filepath.Join(cfg.DataDir, "work")
	for _, d := range []string{filepath.Join(workDir, "enc"), filepath.Join(workDir, "dec")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, nil, err
		}
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "volteye.db"))
	if err != nil {
		return nil, nil, err
	}
	if rules, err := st.ListRules(); err == nil {
		var erules []extract.Rule
		for _, r := range rules {
			var kws []string
			for _, kw := range strings.Split(r.Keywords, ",") {
				if kw = strings.TrimSpace(kw); kw != "" {
					kws = append(kws, kw)
				}
			}
			erules = append(erules, extract.Rule{ID: r.ID, Name: r.Name, Keywords: kws, Regex: r.Regex, Enabled: r.Enabled})
		}
		if err := cfg.Engine.Load(erules); err != nil {
			step("规则编译警告: " + err.Error())
		} else if len(erules) > 0 {
			step(fmt.Sprintf("已加载 %d 条识别规则", len(erules)))
		}
	}

	var roomOrder []string
	names := map[string]string{}
	if sessDB := wechatdb.FindSessionDB(ds); sessDB != "" {
		encCopy := filepath.Join(workDir, "enc", "_session.db")
		decPath := filepath.Join(workDir, "dec", "_session.db")
		if err := wechatdb.CopyFile(sessDB, encCopy); err == nil {
			if _, err := wechatdb.DecryptDB(rawKey, encCopy, decPath); err == nil {
				if db, err := wechatdb.OpenDB(decPath); err == nil {
					roomOrder, _ = wechatdb.ListChatrooms(db)
					db.Close()
				}
			}
		}
	}
	if contactDB := wechatdb.FindContactDB(ds); contactDB != "" {
		encCopy := filepath.Join(workDir, "enc", "_contact.db")
		decPath := filepath.Join(workDir, "dec", "_contact.db")
		if err := wechatdb.CopyFile(contactDB, encCopy); err == nil {
			if _, err := wechatdb.DecryptDB(rawKey, encCopy, decPath); err == nil {
				if db, err := wechatdb.OpenDB(decPath); err == nil {
					names = wechatdb.ChatroomNames(db)
					db.Close()
				}
			}
		}
	}
	for _, wxid := range roomOrder {
		if err := st.UpsertGroup(wxid, names[wxid]); err != nil {
			st.Close()
			return nil, nil, err
		}
	}
	step(fmt.Sprintf("已同步 %d 个群聊（%d 个有名称）", len(roomOrder), len(names)))

	interval := cfg.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	col := sync.New(sync.Config{
		DBStorage: ds,
		Key:       rawKey,
		Interval:  interval,
		WorkDir:   workDir,
		Logf: func(s string) {
			send(logMsg{at: time.Now(), text: s})
		},
		OnPollDone: func(n int) {
			send(pollTickMsg{at: time.Now(), inserted: n})
		},
		Matcher: cfg.Engine,
	}, st)
	if err := col.Init(); err != nil {
		st.Close()
		return nil, nil, err
	}
	cleanup.StartLoop(st, cfg.DataDir, filepath.Join(cfg.DataDir, "volteye.db"), time.Hour, func(s string) {
		send(logMsg{at: time.Now(), text: s})
	})
	return st, col, nil
}
