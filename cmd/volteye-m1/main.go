package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"volteye/internal/capture"
	"volteye/internal/store"
	"volteye/internal/sync"
	"volteye/internal/wechatdb"
)

func fatal(err error) {
	fmt.Println("[-]", err)
	os.Exit(1)
}

func recoverKey(dbStorage string, page1 []byte) ([]byte, error) {
	pid, name, err := wechatdb.FindWeChatPID()
	if err != nil {
		return nil, err
	}
	fmt.Printf("[*] %s PID=%d\n", name, pid)
	var internalKeys [][]byte
	if exePath, err := wechatdb.GetProcessExePath(pid); err == nil {
		if dll, err := wechatdb.FindWeixinDLL(filepath.Dir(exePath)); err == nil {
			internalKeys, _ = wechatdb.ExtractInternalKeys(dll)
		}
	}
	fmt.Printf("[*] %d internal key candidate(s) from dll\n", len(internalKeys))
	fmt.Println("[*] scanning process memory for db key ...")
	return wechatdb.RecoverKey(pid, page1, internalKeys, func(s string) { fmt.Println("    " + s) })
}

func main() {
	var (
		dbStorage = flag.String("dbstorage", "", "db_storage directory path (auto-detect if empty)")
		keyHex    = flag.String("key", "", "64-hex-char db key (skip memory scan)")
		dataDir   = flag.String("data", "data", "data directory for local store")
		interval  = flag.Duration("interval", 3*time.Second, "poll interval")
		monitor   = flag.String("monitor", "", "comma-separated group wxids to monitor")
		backfill  = flag.String("backfill", "", "comma-separated group wxids to backfill history")
		listOnly  = flag.Bool("list", false, "list groups and exit")
		once      = flag.Bool("once", false, "single poll then exit")
		duration  = flag.Duration("duration", 0, "stop after this duration (0 = until Ctrl+C)")
	)
	flag.Parse()

	ds := *dbStorage
	if ds == "" {
		var err error
		ds, err = wechatdb.DefaultDBStorage()
		if err != nil {
			fatal(err)
		}
	}
	fmt.Println("[*] db_storage:", ds)

	verifyDB, page1, err := wechatdb.FindVerifyPage(ds)
	if err != nil {
		fatal(err)
	}
	var rawKey []byte
	if *keyHex != "" {
		rawKey, err = wechatdb.ParseKeyHex(*keyHex)
		if err != nil {
			fatal(err)
		}
		if !wechatdb.VerifyPage1Key(rawKey, page1) {
			fatal(fmt.Errorf("provided key failed verification"))
		}
	} else {
		rawKey, err = recoverKey(ds, page1)
		if err != nil {
			fatal(err)
		}
	}
	fmt.Println("[+] db key ready")
	_ = verifyDB

	workDir := filepath.Join(*dataDir, "work")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		fatal(err)
	}

	names := map[string]string{}
	var roomOrder []string
	if sessDB := wechatdb.FindSessionDB(ds); sessDB != "" {
		encCopy := filepath.Join(workDir, "enc", "_session.db")
		decPath := filepath.Join(workDir, "dec", "_session.db")
		for _, d := range []string{filepath.Dir(encCopy), filepath.Dir(decPath)} {
			if err := os.MkdirAll(d, 0755); err != nil {
				fatal(err)
			}
		}
		if err := wechatdb.CopyFile(sessDB, encCopy); err == nil {
			_, derr := wechatdb.DecryptDB(rawKey, encCopy, decPath)
			os.Remove(encCopy)
			if derr == nil {
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
			_, derr := wechatdb.DecryptDB(rawKey, encCopy, decPath)
			os.Remove(encCopy)
			if derr == nil {
				if db, err := wechatdb.OpenDB(decPath); err == nil {
					names = wechatdb.ChatroomNames(db)
					db.Close()
				}
			}
		}
	}

	st, err := store.Open(filepath.Join(*dataDir, "volteye.db"))
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	for _, wxid := range roomOrder {
		if err := st.UpsertGroup(wxid, names[wxid]); err != nil {
			fatal(err)
		}
	}
	fmt.Printf("[*] %d chatroom(s) synced to store (%d with names)\n", len(roomOrder), len(names))

	if *monitor != "" {
		for _, wxid := range strings.Split(*monitor, ",") {
			wxid = strings.TrimSpace(wxid)
			if wxid == "" {
				continue
			}
			if err := st.SetMonitored(wxid, true); err != nil {
				fatal(err)
			}
			fmt.Println("[+] monitor on:", wxid)
		}
	}
	if *backfill != "" {
		for _, wxid := range strings.Split(*backfill, ",") {
			wxid = strings.TrimSpace(wxid)
			if wxid == "" {
				continue
			}
			if err := st.SetBackfill(wxid, true); err != nil {
				fatal(err)
			}
			fmt.Println("[+] backfill on:", wxid)
		}
	}

	if *listOnly {
		groups, err := st.ListGroups()
		if err != nil {
			fatal(err)
		}
		counts, _ := st.GroupMessageCounts()
		for _, g := range groups {
			mark := "  "
			if g.Monitored {
				mark = "* "
			}
			name := g.Name
			if name == "" {
				name = "(no name)"
			}
			fmt.Printf("%s%-40s %-28s stored=%d\n", mark, g.Wxid, name, counts[g.Wxid])
		}
		total, _ := st.TotalMessages()
		fmt.Printf("[*] total stored messages: %d\n", total)
		return
	}

	monitored, err := st.MonitoredGroups()
	if err != nil {
		fatal(err)
	}
	if len(monitored) == 0 {
		fmt.Println("[-] no groups monitored; use -monitor <wxid,...> first")
		return
	}
	fmt.Printf("[*] monitoring %d group(s), interval %s\n", len(monitored), *interval)
	for _, g := range monitored {
		fmt.Printf("    %s (%s) backfill=%v done=%v\n", g.Wxid, g.Name, g.Backfill, g.BackfillDone)
	}

	reg := capture.NewRegistry()
	if cfgs, err := st.ListFormats(); err == nil {
		if err := reg.Load(cfgs); err != nil {
			fmt.Println("[!] format registry load failed:", err)
		}
	}
	c := sync.New(sync.Config{
		DBStorage: ds,
		Key:       rawKey,
		Interval:  *interval,
		WorkDir:   workDir,
		Logf: func(s string) {
			fmt.Printf("%s %s\n", time.Now().Format("15:04:05"), s)
		},
		Registry: reg,
	}, st)
	if err := c.Init(); err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[*] stopping ...")
		cancel()
	}()
	if *duration > 0 {
		go func() {
			time.Sleep(*duration)
			cancel()
		}()
	}

	if *once {
		n, err := c.PollOnce()
		if err != nil {
			fatal(err)
		}
		fmt.Printf("[*] poll done, +%d message(s)\n", n)
		total, _ := st.TotalMessages()
		fmt.Printf("[*] total stored messages: %d\n", total)
		return
	}

	if err := c.Run(ctx); err != nil && err != context.Canceled {
		fatal(err)
	}
	total, _ := st.TotalMessages()
	fmt.Printf("[*] total stored messages: %d\n", total)
}
