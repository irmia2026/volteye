package sync

import (
	"path/filepath"

	"volteye/internal/store"
	"volteye/internal/wechatdb"
)

func DiscoverGroups(dbStorage string, key []byte, encDir, decDir string, st *store.Store) (total, named int, err error) {
	var roomOrder []string
	names := map[string]string{}
	if sessDB := wechatdb.FindSessionDB(dbStorage); sessDB != "" {
		encCopy := filepath.Join(encDir, "_session.db")
		decPath := filepath.Join(decDir, "_session.db")
		if err := wechatdb.CopyFile(sessDB, encCopy); err == nil {
			if _, err := wechatdb.DecryptDB(key, encCopy, decPath); err == nil {
				if db, err := wechatdb.OpenDB(decPath); err == nil {
					roomOrder, _ = wechatdb.ListChatrooms(db)
					db.Close()
				}
			}
		}
	}
	if contactDB := wechatdb.FindContactDB(dbStorage); contactDB != "" {
		encCopy := filepath.Join(encDir, "_contact.db")
		decPath := filepath.Join(decDir, "_contact.db")
		if err := wechatdb.CopyFile(contactDB, encCopy); err == nil {
			if _, err := wechatdb.DecryptDB(key, encCopy, decPath); err == nil {
				if db, err := wechatdb.OpenDB(decPath); err == nil {
					names = wechatdb.ChatroomNames(db)
					db.Close()
				}
			}
		}
	}
	for _, wxid := range roomOrder {
		if err := st.UpsertGroup(wxid, names[wxid]); err != nil {
			return 0, 0, err
		}
	}
	return len(roomOrder), len(names), nil
}
