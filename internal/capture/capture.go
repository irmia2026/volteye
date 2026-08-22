// Package capture turns raw chat messages into structured work orders.
//
// The pipeline is extensible at two levels:
//   - configuration: a new "bracketed key-value" format is just a FormatConfig
//     row (signature regex + bracket pair + field alias map), no code change;
//   - code: a message shape that is not bracketed KV can provide a new
//     Extractor implementation and register it under a new kind.
package capture

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Message is a normalized raw message flowing through the capture pipeline.
type Message struct {
	ID         int64
	GroupWxid  string
	SenderWxid string
	SrcDB      string
	LocalID    int64
	CreateTime int64
	Content    string
}

// WorkOrder is the canonical structured record extracted from a message.
// Fields left unmapped by a format stay empty; Raw always keeps the source.
type WorkOrder struct {
	OrderNo      string
	Format       string
	Priority     string
	Category     string
	DispatchTime int64
	Address      string
	Description  string
	ContactName  string
	ContactWay   string
	ContactPhone string
	UserNo       string
	UserName     string
	Raw          string

	GroupWxid  string
	SenderWxid string
	SrcDB      string
	LocalID    int64
	CreateTime int64
}

// CategoryLevels splits the normalized category chain ("故障报修/低压停电/一户停电")
// into its levels. Missing category yields nil.
func (w *WorkOrder) CategoryLevels() []string {
	if w.Category == "" {
		return nil
	}
	return strings.Split(w.Category, "/")
}

// Canonical field names usable on the right side of an alias mapping.
var CanonicalFields = []string{
	"order_no", "dispatch_time", "address", "description",
	"contact_name", "contact_way", "contact_phone", "category", "user_no", "user_name",
}

// Extractor recognizes one message shape and parses it into work orders.
type Extractor interface {
	Name() string
	Kind() string
	// Match is a cheap pre-filter before the full parse.
	Match(content string) bool
	// Extract parses m. It returns nil without error when Match fails.
	// A signature hit that cannot yield an order number is an error, so the
	// caller can record a parse failure instead of silently dropping the order.
	Extract(m Message) ([]WorkOrder, error)
}

// FormatConfig is the stored definition of one bracketed-KV message format.
type FormatConfig struct {
	ID          int64
	Name        string
	Kind        string // "bracketkv"
	Signature   string // regex recognizing the message
	OpenB       string // opening bracket, e.g. 【 or [
	CloseB      string // closing bracket, e.g. 】 or ]
	Aliases     string // one "源字段=canonical" per line
	CategoryKey string // optional chain marker, e.g. 业务类型为
	Enabled     bool
}

// ParseAliases parses the "源字段=canonical" line format.
func ParseAliases(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// FormatAliases renders an alias map back to the line format.
func FormatAliases(m map[string]string, order []string) string {
	var sb strings.Builder
	for _, k := range order {
		if v, ok := m[k]; ok {
			fmt.Fprintf(&sb, "%s=%s\n", k, v)
		}
	}
	for k, v := range m {
		found := false
		for _, o := range order {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(&sb, "%s=%s\n", k, v)
		}
	}
	return sb.String()
}

// CompileFormat builds an Extractor from a stored config.
func CompileFormat(cfg FormatConfig) (Extractor, error) {
	switch cfg.Kind {
	case "", "bracketkv":
		return compileBracketKV(cfg)
	default:
		return nil, fmt.Errorf("format %q: unknown kind %q", cfg.Name, cfg.Kind)
	}
}

// Registry holds the active extractors and is safe for concurrent use.
type Registry struct {
	mu         sync.RWMutex
	extractors []Extractor
}

func NewRegistry() *Registry { return &Registry{} }

// Load replaces all extractors. A config that fails to compile aborts the
// whole load so a broken edit never silently disables the other formats.
func (r *Registry) Load(cfgs []FormatConfig) error {
	var exs []Extractor
	for _, c := range cfgs {
		if !c.Enabled {
			continue
		}
		ex, err := CompileFormat(c)
		if err != nil {
			return err
		}
		exs = append(exs, ex)
	}
	r.mu.Lock()
	r.extractors = exs
	r.mu.Unlock()
	return nil
}

func (r *Registry) Extractors() []Extractor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Extractor, len(r.extractors))
	copy(out, r.extractors)
	return out
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.extractors)
}

// Extract runs every matching extractor and aggregates the results. The first
// extractor error is returned together with the orders parsed so far.
func (r *Registry) Extract(m Message) ([]WorkOrder, error) {
	var orders []WorkOrder
	for _, ex := range r.Extractors() {
		if !ex.Match(m.Content) {
			continue
		}
		os, err := ex.Extract(m)
		if err != nil {
			return orders, fmt.Errorf("format %s: %w", ex.Name(), err)
		}
		orders = append(orders, os...)
	}
	return orders, nil
}

// ---------------------------------------------------------------------------
// bracketkv extractor
// ---------------------------------------------------------------------------

type bracketKV struct {
	cfg     FormatConfig
	sig     *regexp.Regexp
	aliases map[string]string
	open    rune
	close   rune
	catRe   *regexp.Regexp
}

func compileBracketKV(cfg FormatConfig) (*bracketKV, error) {
	if cfg.Signature == "" {
		return nil, fmt.Errorf("format %q: empty signature", cfg.Name)
	}
	sig, err := regexp.Compile(cfg.Signature)
	if err != nil {
		return nil, fmt.Errorf("format %q: bad signature: %w", cfg.Name, err)
	}
	open := []rune(cfg.OpenB)
	cls := []rune(cfg.CloseB)
	if len(open) != 1 || len(cls) != 1 {
		return nil, fmt.Errorf("format %q: brackets must be single characters", cfg.Name)
	}
	e := &bracketKV{
		cfg:     cfg,
		sig:     sig,
		aliases: ParseAliases(cfg.Aliases),
		open:    open[0],
		close:   cls[0],
	}
	if cfg.CategoryKey != "" {
		e.catRe = buildChainRe(cfg.CategoryKey, open[0], cls[0])
	}
	return e, nil
}

func (e *bracketKV) Name() string { return e.cfg.Name }
func (e *bracketKV) Kind() string { return "bracketkv" }

func (e *bracketKV) Match(content string) bool {
	return content != "" && e.sig.MatchString(content)
}

var errNoOrderNo = fmt.Errorf("签名命中但未解析出工单号")

func (e *bracketKV) Extract(m Message) ([]WorkOrder, error) {
	if !e.Match(m.Content) {
		return nil, nil
	}
	wo := WorkOrder{
		Format:     e.cfg.Name,
		Raw:        m.Content,
		GroupWxid:  m.GroupWxid,
		SenderWxid: m.SenderWxid,
		SrcDB:      m.SrcDB,
		LocalID:    m.LocalID,
		CreateTime: m.CreateTime,
	}
	for _, p := range parseBracketKV(m.Content, e.open, e.close) {
		field, ok := e.aliases[p.Key]
		if !ok {
			continue
		}
		wo.setField(field, p.Value)
	}
	if e.catRe != nil {
		if mm := e.catRe.FindStringSubmatch(m.Content); mm != nil {
			wo.Priority = strings.TrimSpace(mm[1])
			wo.Category = joinChain(mm[2], e.open, e.close)
		}
	}
	if wo.OrderNo == "" {
		return nil, errNoOrderNo
	}
	return []WorkOrder{wo}, nil
}

func (w *WorkOrder) setField(field, value string) {
	switch field {
	case "order_no":
		w.OrderNo = value
	case "dispatch_time":
		w.DispatchTime = parseTime(value)
	case "address":
		w.Address = value
	case "description":
		w.Description = value
	case "contact_name":
		w.ContactName = value
	case "contact_way":
		w.ContactWay = value
	case "contact_phone":
		w.ContactPhone = value
	case "category":
		// KV path yields "意见--其他"; the chain path already joins with "/".
		w.Category = strings.ReplaceAll(value, "--", "/")
	case "user_no":
		w.UserNo = value
	case "user_name":
		w.UserName = value
	}
}

// Pair is one raw key:value segment of a bracketed message.
type Pair struct {
	Key   string
	Value string
}

// keySeparators delimit the key fragment from the surrounding boilerplate.
const keySeparators = "，。：；、！!？?\n\r\t "

// parseBracketKV scans content for "key(为)?：(open)value(close)" segments.
// The key is the last separator-delimited fragment before the opening bracket
// with a trailing "为" stripped, so both "故障单号为：【x】" and "故障地址：【x】"
// yield clean keys. Pairs with an empty key (e.g. the 【南方电网】 prefix
// tokens) are skipped.
//
// Values may contain nested same-type brackets (e.g. a 备注 like "[经解释...]"
// inside 诉求内容): the close is matched by bracket depth. If the depth never
// returns to zero (an unbalanced extra open inside the value), parsing falls
// back to the first shallow close so one bad value does not swallow the rest
// of the message.
func parseBracketKV(content string, open, close rune) []Pair {
	runes := []rune(content)
	var pairs []Pair
	prevEnd := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] != open {
			continue
		}
		j := i + 1
		depth := 1
		firstClose := -1
		for j < len(runes) {
			switch runes[j] {
			case open:
				depth++
			case close:
				depth--
				if firstClose < 0 {
					firstClose = j
				}
				if depth == 0 {
					goto matched
				}
			}
			j++
		}
		// Unbalanced: fall back to the first shallow close, or give up.
		if firstClose < 0 {
			break
		}
		j = firstClose
	matched:
		key := cleanKey(string(runes[prevEnd:i]))
		value := strings.TrimSpace(string(runes[i+1 : j]))
		if key != "" {
			pairs = append(pairs, Pair{Key: key, Value: value})
		}
		prevEnd = j + 1
		i = j
	}
	return pairs
}

func cleanKey(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return strings.ContainsRune(keySeparators, r)
	})
	key := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			key = parts[i]
			break
		}
	}
	key = strings.TrimSuffix(key, "为")
	return key
}

// buildChainRe matches a priority token followed by a category chain, e.g.
// [一般]-业务类型为：-[故障报修]--[低压停电]--[一户停电]--[其它原因]
// Submatch 1 is the priority, submatch 2 the whole bracket chain.
func buildChainRe(key string, open, close rune) *regexp.Regexp {
	o := regexp.QuoteMeta(string(open))
	c := regexp.QuoteMeta(string(close))
	tok := o + `[^` + c + `]*` + c
	re := o + `([^` + c + `]*)` + c + `-` + regexp.QuoteMeta(key) + `：-(` + tok + `(?:--` + tok + `)*)`
	return regexp.MustCompile(re)
}

// joinChain turns "[故障报修]--[低压停电]--[一户停电]" into "故障报修/低压停电/一户停电".
func joinChain(chain string, open, close rune) string {
	var parts []string
	for _, seg := range strings.Split(chain, "--") {
		seg = strings.TrimSpace(seg)
		seg = strings.TrimPrefix(seg, string(open))
		seg = strings.TrimSuffix(seg, string(close))
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

var timeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006年1月2日 15:04:05",
	"2006年1月2日 15:04",
}

// parseTime parses a dispatch timestamp in local time; unparseable input
// yields 0 and the raw value is still available in Raw.
func parseTime(s string) int64 {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// built-in formats
// ---------------------------------------------------------------------------

// DefaultFormats returns the known CSG message shapes, used to seed the
// formats table. Signatures anchor on the bracketed key alone (not the
// 【南方电网】 prefix): real traffic includes 【海南电网】 and prefix-less
// forwards, and "工作单号为：[" is specific enough by itself.
func DefaultFormats() []FormatConfig {
	return []FormatConfig{
		{
			Name:      "csg-a-故障单",
			Kind:      "bracketkv",
			Signature: `故障单号为：【`,
			OpenB:     "【",
			CloseB:    "】",
			Aliases: "故障单号=order_no\n" +
				"工单派工时间=dispatch_time\n" +
				"故障地址=address\n" +
				"报障描述=description\n" +
				"联系人=contact_name\n" +
				"联系电话=contact_phone\n",
			Enabled: true,
		},
		{
			Name:      "csg-b-工作单",
			Kind:      "bracketkv",
			Signature: `工作单号为：\[`,
			OpenB:     "[",
			CloseB:    "]",
			Aliases: "工作单号=order_no\n" +
				"工单派工时间=dispatch_time\n" +
				"地址=address\n" +
				"诉求内容=description\n" +
				"联系人=contact_name\n" +
				"联系方式=contact_way\n" +
				"联系内容=contact_phone\n" +
				"业务类型=category\n" +
				"用户编号=user_no\n" +
				"用户名称=user_name\n",
			CategoryKey: "业务类型为",
			Enabled:     true,
		},
		{
			// 同 csg-b 的字段结构,但使用书名号括号(混合变体)
			Name:      "csg-c-工作单(书括号)",
			Kind:      "bracketkv",
			Signature: `工作单号为：【`,
			OpenB:     "【",
			CloseB:    "】",
			Aliases: "工作单号=order_no\n" +
				"工单派工时间=dispatch_time\n" +
				"地址=address\n" +
				"诉求内容=description\n" +
				"联系人=contact_name\n" +
				"联系方式=contact_way\n" +
				"联系内容=contact_phone\n" +
				"业务类型=category\n" +
				"用户编号=user_no\n" +
				"用户名称=user_name\n",
			CategoryKey: "业务类型为",
			Enabled:     true,
		},
		{
			// 同 csg-b 的字段结构,但使用半角圆括号
			Name:      "csg-d-工作单(圆括号)",
			Kind:      "bracketkv",
			Signature: `工作单号为：\(`,
			OpenB:     "(",
			CloseB:    ")",
			Aliases: "工作单号=order_no\n" +
				"工单派工时间=dispatch_time\n" +
				"地址=address\n" +
				"诉求内容=description\n" +
				"联系人=contact_name\n" +
				"联系方式=contact_way\n" +
				"联系内容=contact_phone\n" +
				"业务类型=category\n" +
				"用户编号=user_no\n" +
				"用户名称=user_name\n",
			CategoryKey: "业务类型为",
			Enabled:     true,
		},
		{
			// 电网管理平台-配网故障抢修管理 的报障单(KF 单号)
			Name:      "csg-e-报障单",
			Kind:      "bracketkv",
			Signature: `报障单号为：【`,
			OpenB:     "【",
			CloseB:    "】",
			Aliases: "报障单号=order_no\n" +
				"故障单发生时间=dispatch_time\n" +
				"故障地址=address\n" +
				"报障描述=description\n" +
				"联系电话=contact_phone\n",
			Enabled: true,
		},
	}
}
