package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"volteye/internal/cleanup"
	"volteye/internal/export"
	"volteye/internal/extract"
	"volteye/internal/store"
	"volteye/internal/sync"
	"volteye/internal/wechatdb"
)

type Event struct {
	At       time.Time
	Text     string
	Poll     bool
	Inserted int
}

type Config struct {
	DBStorage string
	DataDir   string
	KeyHex    string
	Interval  time.Duration
	OnEvent   func(Event)
}

type Service struct {
	St      *store.Store
	Col     *sync.Collector
	Engine  *extract.Engine
	DataDir string
	DBPath  string
	ExePath string

	onEvent func(Event)
	cancel  context.CancelFunc
	started atomic.Bool
	stopped atomic.Bool
}

func NewService(st *store.Store, col *sync.Collector, engine *extract.Engine, dataDir string, onEvent func(Event)) *Service {
	if engine == nil {
		engine = extract.NewEngine()
	}
	exe, _ := os.Executable()
	return &Service{
		St:      st,
		Col:     col,
		Engine:  engine,
		DataDir: dataDir,
		DBPath:  filepath.Join(dataDir, "volteye.db"),
		ExePath: exe,
		onEvent: onEvent,
	}
}

func (s *Service) emit(ev Event) {
	if s.onEvent != nil && !s.stopped.Load() {
		s.onEvent(ev)
	}
}

func Boot(cfg Config, step func(string)) (*Service, error) {
	if step == nil {
		step = func(string) {}
	}

	ds := cfg.DBStorage
	if ds == "" {
		var err error
		ds, err = wechatdb.DefaultDBStorage()
		if err != nil {
			return nil, err
		}
	}
	step("数据目录: " + ds)

	_, page1, err := wechatdb.FindVerifyPage(ds)
	if err != nil {
		return nil, err
	}

	var rawKey []byte
	if cfg.KeyHex != "" {
		rawKey, err = wechatdb.ParseKeyHex(cfg.KeyHex)
		if err != nil {
			return nil, err
		}
		if !wechatdb.VerifyPage1Key(rawKey, page1) {
			return nil, fmt.Errorf("手动提供的密钥校验失败")
		}
		step("手动密钥校验通过")
	} else {
		pid, name, err := wechatdb.FindWeChatPID()
		if err != nil {
			return nil, err
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
			return nil, err
		}
		step(fmt.Sprintf("密钥提取成功，耗时 %s", time.Since(t0).Round(time.Millisecond)))
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, err
	}
	probe, err := os.CreateTemp(cfg.DataDir, ".write-test-*")
	if err != nil {
		return nil, fmt.Errorf("数据目录不可写: %v", err)
	}
	probe.Close()
	os.Remove(probe.Name())

	workDir := filepath.Join(cfg.DataDir, "work")
	encDir := filepath.Join(workDir, "enc")
	decDir := filepath.Join(workDir, "dec")
	for _, d := range []string{encDir, decDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, err
		}
	}

	dbPath := filepath.Join(cfg.DataDir, "volteye.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	svc := NewService(st, nil, nil, cfg.DataDir, cfg.OnEvent)
	if err := svc.ReloadRules(); err != nil {
		step("规则编译警告: " + err.Error())
	} else if n := svc.Engine.Count(); n > 0 {
		step(fmt.Sprintf("已加载 %d 条识别规则", n))
	}

	total, named, err := sync.DiscoverGroups(ds, rawKey, encDir, decDir, st)
	if err != nil {
		st.Close()
		return nil, err
	}
	step(fmt.Sprintf("已同步 %d 个群聊（%d 个有名称）", total, named))

	interval := cfg.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	if !IsElevated() {
		step("警告：当前非管理员运行，若密钥提取失败请以管理员身份重启")
	}
	col := sync.New(sync.Config{
		DBStorage: ds,
		Key:       rawKey,
		Interval:  interval,
		WorkDir:   workDir,
		Logf: func(s string) {
			svc.emit(Event{At: time.Now(), Text: s})
		},
		OnPollDone: func(n int) {
			svc.emit(Event{At: time.Now(), Poll: true, Inserted: n})
		},
		Matcher: svc.Engine,
	}, st)
	if v, err := strconv.Atoi(st.GetSetting("poll_interval_ms", "")); err == nil && v > 0 {
		col.SetInterval(time.Duration(v) * time.Millisecond)
	}
	if err := col.Init(); err != nil {
		st.Close()
		return nil, err
	}
	svc.Col = col
	return svc, nil
}

func (s *Service) Start() {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	if s.Col != nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		go func() { _ = s.Col.Run(ctx) }()
	}
	cleanup.StartLoop(s.St, s.DataDir, s.DBPath, time.Hour, func(x string) {
		s.emit(Event{At: time.Now(), Text: x})
	})
}

func (s *Service) Stop() {
	s.stopped.Store(true)
	if s.cancel != nil {
		s.cancel()
	}
	if s.St != nil {
		s.St.Close()
	}
}

func (s *Service) ReloadRules() error {
	rules, err := s.St.ListRules()
	if err != nil {
		return err
	}
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
	return s.Engine.Load(erules)
}

func (s *Service) AddRule(name, keywords, regex string) error {
	name = strings.TrimSpace(name)
	keywords = strings.TrimSpace(keywords)
	regex = strings.TrimSpace(regex)
	if name == "" {
		return fmt.Errorf("规则名称不能为空")
	}
	if keywords == "" && regex == "" {
		return fmt.Errorf("关键词和正则至少填写一项")
	}
	if regex != "" {
		probe := extract.NewEngine()
		if err := probe.Load([]extract.Rule{{Name: "probe", Regex: regex, Enabled: true}}); err != nil {
			return fmt.Errorf("正则无效: %v", err)
		}
	}
	if _, err := s.St.AddRule(name, keywords, regex); err != nil {
		return err
	}
	return s.ReloadRules()
}

func (s *Service) DeleteRule(id int64) error {
	if err := s.St.DeleteRule(id); err != nil {
		return err
	}
	return s.ReloadRules()
}

func (s *Service) SetRuleEnabled(id int64, enabled bool) error {
	if err := s.St.SetRuleEnabled(id, enabled); err != nil {
		return err
	}
	return s.ReloadRules()
}

func (s *Service) RescanAll() (int64, error) {
	return s.St.ResetScanned()
}

func (s *Service) ExportMessages(opts export.Options, outPath string) (int, error) {
	return export.MessagesXLSX(s.St, opts, outPath)
}

func (s *Service) ClearAllMessages() (int64, string, error) {
	total, err := s.St.TotalMessages()
	if err != nil {
		return 0, "", err
	}
	archived := ""
	if total > 0 {
		n, path, err := cleanup.ArchiveAll(s.St, s.DataDir)
		if err != nil {
			return 0, "", fmt.Errorf("归档失败，已取消清空: %v", err)
		}
		if n > 0 {
			archived = path
		}
	}
	n, err := s.St.DeleteAllMessages()
	if err != nil {
		return 0, archived, err
	}
	return n, archived, nil
}

func (s *Service) RunCleanup() []string {
	var logs []string
	cleanup.RunOnce(s.St, s.DataDir, s.DBPath, func(x string) { logs = append(logs, x) })
	return logs
}

func (s *Service) PollInterval() time.Duration {
	if s.Col != nil {
		return s.Col.Interval()
	}
	if v, err := strconv.Atoi(s.St.GetSetting("poll_interval_ms", "")); err == nil && v > 0 {
		return time.Duration(v) * time.Millisecond
	}
	return 3 * time.Second
}

func (s *Service) SetPollInterval(d time.Duration) error {
	if s.Col != nil {
		s.Col.SetInterval(d)
	}
	return s.St.SetSetting("poll_interval_ms", strconv.FormatInt(d.Milliseconds(), 10))
}

func (s *Service) SetRetentionDays(days int) error {
	return s.St.SetSetting("retention_days", strconv.Itoa(days))
}

func (s *Service) SetMaxDBMB(mb int) error {
	return s.St.SetSetting("max_db_mb", strconv.Itoa(mb))
}

func (s *Service) SetAutoStart(enable bool) error {
	return SetAutoStart(enable, s.ExePath)
}

func (s *Service) TrayEnabled() bool {
	return s.St.GetSetting("tray_enabled", "0") == "1"
}

func (s *Service) SetTrayEnabled(enable bool) error {
	v := "0"
	if enable {
		v = "1"
	}
	return s.St.SetSetting("tray_enabled", v)
}

func (s *Service) RefreshGroups() (int, int, error) {
	if s.Col == nil {
		return 0, 0, fmt.Errorf("采集器未启动")
	}
	return s.Col.RefreshGroups()
}
