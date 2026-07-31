package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"volteye/internal/wechatdb"
)

func fatal(err error) {
	fmt.Println("[-]", err)
	os.Exit(1)
}

func main() {
	var (
		dbStorage = flag.String("dbstorage", "", "db_storage directory path (auto-detect if empty)")
		group     = flag.String("group", "", "group wxid ending with @chatroom to dump messages from")
		keyHex    = flag.String("key", "", "64-hex-char db key (skip memory scan)")
		nMsg      = flag.Int("n", 10, "number of latest messages to print")
		outDir    = flag.String("out", "m0_work", "working dir for decrypted copies")
		tryWAL    = flag.Bool("wal", true, "also decrypt -wal files (experimental realtime)")
	)
	flag.Parse()

	fmt.Println("== VoltEye M0 tech validation ==")

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
	fmt.Println("[*] verify db:", verifyDB)

	var rawKey []byte
	if *keyHex != "" {
		rawKey, err = wechatdb.ParseKeyHex(*keyHex)
		if err != nil {
			fatal(err)
		}
		if !wechatdb.VerifyPage1Key(rawKey, page1) {
			fatal(fmt.Errorf("provided key failed page-1 HMAC verification"))
		}
		fmt.Println("[+] provided key verified against page 1")
	} else {
		pid, name, err := wechatdb.FindWeChatPID()
		if err != nil {
			fatal(err)
		}
		fmt.Printf("[*] %s PID=%d\n", name, pid)
		fmt.Println("[*] scanning process memory for db key ...")
		t0 := time.Now()
		rawKey, err = wechatdb.RecoverKey(pid, page1, func(s string) { fmt.Println("    " + s) })
		if err != nil {
			fatal(err)
		}
		fmt.Printf("[+] key recovered and verified in %s (value redacted)\n", time.Since(t0).Round(time.Millisecond))
	}

	encDir := filepath.Join(*outDir, "enc")
	decDir := filepath.Join(*outDir, "dec")
	if err := os.MkdirAll(encDir, 0755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(decDir, 0755); err != nil {
		fatal(err)
	}

	if sessDB := wechatdb.FindSessionDB(ds); sessDB != "" {
		encCopy := filepath.Join(encDir, "_session.db")
		decPath := filepath.Join(decDir, "_session.db")
		if err := wechatdb.CopyFile(sessDB, encCopy); err == nil {
			if _, err := wechatdb.DecryptDB(rawKey, encCopy, decPath); err == nil {
				if db, err := wechatdb.OpenDB(decPath); err == nil {
					rooms, err := wechatdb.ListChatrooms(db)
					db.Close()
					if err == nil {
						fmt.Printf("[*] %d chatroom session(s) (use -group with one of these):\n", len(rooms))
						limit := len(rooms)
						if limit > 30 {
							limit = 30
						}
						for _, r := range rooms[:limit] {
							fmt.Println("     ", r)
						}
					}
				}
			}
		}
	}

	msgDBs, err := wechatdb.ListMessageDBs(ds)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("[*] %d message db file(s)\n", len(msgDBs))

	type decEntry struct {
		name string
		path string
	}
	var decs []decEntry
	for _, src := range msgDBs {
		base := filepath.Base(src)
		encCopy := filepath.Join(encDir, base)
		if err := wechatdb.CopyFile(src, encCopy); err != nil {
			fmt.Println("    [!] copy failed:", base, err)
			continue
		}
		decPath := filepath.Join(decDir, base)
		t0 := time.Now()
		pages, err := wechatdb.DecryptDB(rawKey, encCopy, decPath)
		if err != nil {
			fmt.Println("    [!] decrypt failed:", base, err)
			continue
		}
		fmt.Printf("    [+] %-18s %8d pages  %s\n", base, pages, time.Since(t0).Round(time.Millisecond))

		if *tryWAL {
			walSrc := src + "-wal"
			if st, err := os.Stat(walSrc); err == nil && st.Size() > int64(wechatdb.PageSize) {
				walCopy := encCopy + "-wal"
				if err := wechatdb.CopyFile(walSrc, walCopy); err == nil {
					frames, err := wechatdb.DecryptWAL(rawKey, encCopy, walCopy, decPath+"-wal")
					if err != nil {
						fmt.Println("    [!] wal decrypt failed:", base, err)
					} else {
						fmt.Printf("    [+] %-18s %8d wal frames decrypted\n", base+"-wal", frames)
					}
				}
			}
		}
		decs = append(decs, decEntry{base, decPath})
	}

	foundGroup := *group == ""
	for _, d := range decs {
		db, err := wechatdb.OpenDB(d.path)
		if err != nil {
			fmt.Println("    [!] open failed:", d.name, err)
			continue
		}
		tables, err := wechatdb.ListChatTables(db)
		if err != nil {
			fmt.Println("    [!] list tables failed:", d.name, err)
			db.Close()
			continue
		}
		var total int64
		for _, t := range tables {
			total += t.Rows
		}
		fmt.Printf("[*] %-18s %d chat table(s), %d total row(s)\n", d.name, len(tables), total)
		limit := len(tables)
		if limit > 5 {
			limit = 5
		}
		for _, t := range tables[:limit] {
			fmt.Printf("        %s  %d rows\n", t.Name, t.Rows)
		}

		if *group != "" {
			want := wechatdb.MsgTableName(*group)
			for _, t := range tables {
				if t.Name != want {
					continue
				}
				foundGroup = true
				nameMap := wechatdb.Name2ID(db)
				msgs, err := wechatdb.ReadLastMessages(db, t.Name, *nMsg)
				if err != nil {
					fmt.Println("    [!] read messages failed:", err)
					break
				}
				fmt.Printf("[*] latest %d message(s) of %s (%s):\n", len(msgs), *group, t.Name)
				for _, m := range msgs {
					ts := time.Unix(m.CreateTime, 0).Format("01-02 15:04:05")
					sender := m.SenderWxid
					if sender == "" {
						sender = nameMap[m.SenderID]
					}
					if sender == "" {
						sender = fmt.Sprintf("id:%d", m.SenderID)
					}
					body := wechatdb.TypeBrief(m.LocalType)
					if m.Content != "" {
						if body != "" {
							body += " "
						}
						body += wechatdb.Preview(m.Content, 80)
					}
					fmt.Printf("    %s  %-24s  %s\n", ts, sender, body)
				}
			}
		}
		db.Close()
	}
	if !foundGroup {
		fmt.Printf("[!] group %s table not found in decrypted dbs\n", *group)
	}
	fmt.Println("== M0 done ==")
}
