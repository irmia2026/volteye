package capture

import (
	"strings"
	"testing"
	"time"
)

const sampleA = "【南方电网】【电网管理平台】尊敬的用户：您有一条新的工单，请及时处理！故障单号为：【DY2026080218290204xxx】，工单派工时间为：【2026-08-02 18:29】，故障地址：【海南省省直辖县级行政区定安县】，报障描述：【客户来电反映一户停电问题，已建议请电工检查表后线路，客户不接受，要求工作人员尽快联系处理。】，联系人：【女士】，联系电话：【13876655xxx】。"

const sampleB = "【南方电网】尊敬的用户：您有一条[新]工单到达，请及时处理！[一般]-业务类型为：-[故障报修]--[低压停电]--[一户停电]--[其它原因]，工作单号为：[SL070020260803657516]，工单派工时间为：[2026-08-03 08:41:09]，地址为：[海南省省直辖县级行政区定安县]，联系人为：[先生]，联系方式为：[手机]，联系内容为：[139763219xx]，诉求内容为：[客户反映上址一户停电，客户自查不出原因，经系统查询无停电信息，客户需要恢复供电，请联系客户处理。]，用户编号：[0708003000009775]，用户名称：[占]"

// 格式B变体: 诉求内容内嵌套 [...] 且含换行
const sampleBNested = "【南方电网】尊敬的用户：您有一条[新]工单到达，请及时处理！[一般]-业务类型为：-[意见]--[营销服务]--[抄核收服务]--[其他]，工作单号为：[SL070020260814260287]，工单派工时间为：[2026-08-14 16:24:50]，地址为：[南海农场六区五队]，联系人为：[先生]，联系方式为：[手机]，联系内容为：[17508903508]，诉求内容为：[客户反映前单电费串单情况，一月至四月已配合处理完毕，现发现五月账单仍有欠费。（客户1月之后一直未入住）\n坐席已解释，客户不接受，要求工作人员尽快答复，协助处理。请尽快联系客户跟进处理。\n[经解释安抚后客户仍要求继续反映]]，用户编号：[0708003000008498]，用户名称：[陈永花]"

// 混合变体: csg-b 字段结构但使用书名号括号, 分类链仅三级且末尾多一个 "-"
const sampleC = "【南方电网】尊敬的用户：您有一条【新】工单到达，请及时处理！【一般】-业务类型为：-【业务办理】--【用检业务】--【电量异常检测】-，工作单号为：【SL070020260803672389】，工单派工时间为：【2026-08-03 13:12:15】，地址为：【海南省定安县龙湖镇下村】，联系人为：【先生】，联系方式为：【手机】，联系内容为：【13627578025】，诉求内容为：【【应急受理】客户反映7月电量电费异常，客户表示现在表读数与我司电费抄表读数不符，现在表读数为11510，账单读数为11555.71，坐席已解释建议客户核实有无按轮显键切换显示内容，有无核实电表是否为自家电表，客户表示已核实为自家电表确定是当前表读数且已拍照，要求我司安排工作人员核实，请处理】，用户编号：【0708030005376735】，用户名称：【李强】"

// 变体: 分类为单括号形态 "业务类型为：[意见--其他]", 无优先级前缀
const sampleBSingleCat = "【南方电网】尊敬的用户：您有一条新的工单到达，请及时处理！业务类型为：[意见--其他]，工作单号为：[07000010000053444552]，工单派工时间为：[2026-08-14 10:23:47]，应办结时限为：[2026-08-19 09:43:05.0]，地址为：[海南省定安县黄竹镇大坡村]，联系人为：[先生]，联系方式为：[手机]，联系内容为：[19977929959]，诉求内容为：[诉求内容：DH26081446000001148/市长热线/诉求人来电反映：其在定安县黄竹镇大坡村种植槟榔，当地供电部门能否把电线拉到河边，然后农户装电表，方便种植槟榔。]。"

// 变体: 诉求内容以连续嵌套括号开头 [[应急受理][预付费]...
const sampleBLeadingNest = "【南方电网】尊敬的用户：您有一条[新]工单到达，请及时处理！[一般]-业务类型为：-[咨询查询]--[营销服务]--[客户档案基本信息]--[信息查询]，工作单号为：[SL070020260813204603]，工单派工时间为：[2026-08-13 14:29:14]，地址为：[海南省省直辖县级行政区定安县龙湖镇永丰村委会双根水库]，联系人为：[先生]，联系方式为：[手机]，联系内容为：[13876015997]，诉求内容为：[[应急受理][预付费]客户来电咨询户号，仅提供地址和资产编号，信息验证不通过，要求我司协助核实户号，请处理\n]，用户编号：[0708030122450484]，用户名称：[林叶红]"

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	if err := r.Load(DefaultFormats()); err != nil {
		t.Fatal(err)
	}
	return r
}

func msg(content string) Message {
	return Message{
		ID: 1, GroupWxid: "g@chatroom", SenderWxid: "sender",
		SrcDB: "message_0.db", LocalID: 42, CreateTime: 1780000000,
		Content: content,
	}
}

func TestFormatAFullExtraction(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleA))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.OrderNo != "DY2026080218290204xxx" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	if wo.Format != "csg-a-故障单" {
		t.Errorf("format = %q", wo.Format)
	}
	wantTime := time.Date(2026, 8, 2, 18, 29, 0, 0, time.Local).Unix()
	if wo.DispatchTime != wantTime {
		t.Errorf("dispatch_time = %d, want %d", wo.DispatchTime, wantTime)
	}
	if wo.Address != "海南省省直辖县级行政区定安县" {
		t.Errorf("address = %q", wo.Address)
	}
	if !strings.Contains(wo.Description, "一户停电问题") {
		t.Errorf("description = %q", wo.Description)
	}
	if wo.ContactName != "女士" {
		t.Errorf("contact_name = %q", wo.ContactName)
	}
	if wo.ContactPhone != "13876655xxx" {
		t.Errorf("contact_phone = %q", wo.ContactPhone)
	}
	if wo.Raw != sampleA || wo.GroupWxid != "g@chatroom" || wo.LocalID != 42 {
		t.Errorf("trace fields not carried: %+v", wo)
	}
}

func TestFormatBFullExtraction(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleB))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.OrderNo != "SL070020260803657516" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	if wo.Format != "csg-b-工作单" {
		t.Errorf("format = %q", wo.Format)
	}
	wantTime := time.Date(2026, 8, 3, 8, 41, 9, 0, time.Local).Unix()
	if wo.DispatchTime != wantTime {
		t.Errorf("dispatch_time = %d, want %d", wo.DispatchTime, wantTime)
	}
	if wo.Priority != "一般" {
		t.Errorf("priority = %q", wo.Priority)
	}
	if wo.Category != "故障报修/低压停电/一户停电/其它原因" {
		t.Errorf("category = %q", wo.Category)
	}
	if wo.ContactName != "先生" {
		t.Errorf("contact_name = %q", wo.ContactName)
	}
	if wo.ContactPhone != "139763219xx" {
		t.Errorf("contact_phone = %q", wo.ContactPhone)
	}
	if wo.UserNo != "0708003000009775" || wo.UserName != "占" {
		t.Errorf("user = %q/%q", wo.UserNo, wo.UserName)
	}
	if !strings.Contains(wo.Description, "恢复供电") {
		t.Errorf("description = %q", wo.Description)
	}
}

func TestFormatBNestedBrackets(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleBNested))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.OrderNo != "SL070020260814260287" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	if wo.Category != "意见/营销服务/抄核收服务/其他" {
		t.Errorf("category = %q", wo.Category)
	}
	if wo.Address != "南海农场六区五队" {
		t.Errorf("address = %q", wo.Address)
	}
	if wo.ContactPhone != "17508903508" {
		t.Errorf("contact_phone = %q", wo.ContactPhone)
	}
	if wo.UserName != "陈永花" || wo.UserNo != "0708003000008498" {
		t.Errorf("user = %q/%q", wo.UserName, wo.UserNo)
	}
	// 嵌套的 [经解释安抚后客户仍要求继续反映] 必须完整保留在描述里
	if !strings.Contains(wo.Description, "[经解释安抚后客户仍要求继续反映]") {
		t.Errorf("nested bracket truncated: %q", wo.Description)
	}
	if !strings.Contains(wo.Description, "五月账单仍有欠费") {
		t.Errorf("description body lost: %q", wo.Description)
	}
	// 描述之后的字段不能被嵌套括号吃掉
	if wo.Description == "" || wo.UserNo == "" {
		t.Errorf("fields after nested-bracket value broken: %+v", wo)
	}
}

func TestFormatCBookBrackets(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleC))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.Format != "csg-c-工作单(书括号)" {
		t.Errorf("format = %q", wo.Format)
	}
	if wo.OrderNo != "SL070020260803672389" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	if wo.Priority != "一般" {
		t.Errorf("priority = %q", wo.Priority)
	}
	if wo.Category != "业务办理/用检业务/电量异常检测" {
		t.Errorf("category = %q", wo.Category)
	}
	if wo.ContactPhone != "13627578025" {
		t.Errorf("contact_phone = %q", wo.ContactPhone)
	}
	if wo.UserName != "李强" || wo.UserNo != "0708030005376735" {
		t.Errorf("user = %q/%q", wo.UserName, wo.UserNo)
	}
	if !strings.Contains(wo.Description, "【应急受理】") || !strings.Contains(wo.Description, "11555.71") {
		t.Errorf("description broken: %q", wo.Description)
	}
}

func TestFormatBSingleBracketCategory(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleBSingleCat))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.OrderNo != "07000010000053444552" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	// 单括号分类 "意见--其他" 归一化为 "意见/其他"
	if wo.Category != "意见/其他" {
		t.Errorf("category = %q", wo.Category)
	}
	if wo.ContactPhone != "19977929959" {
		t.Errorf("contact_phone = %q", wo.ContactPhone)
	}
	if !strings.Contains(wo.Description, "DH26081446000001148") {
		t.Errorf("description = %q", wo.Description)
	}
}

func TestFormatBLeadingNestedBrackets(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleBLeadingNest))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.OrderNo != "SL070020260813204603" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	if wo.Category != "咨询查询/营销服务/客户档案基本信息/信息查询" {
		t.Errorf("category = %q", wo.Category)
	}
	if !strings.HasPrefix(wo.Description, "[应急受理][预付费]") {
		t.Errorf("description lost leading nested brackets: %q", wo.Description)
	}
	if wo.UserName != "林叶红" {
		t.Errorf("user_name = %q", wo.UserName)
	}
}

// 变体: 半角圆括号
const sampleDParen = "【南方电网】尊敬的用户：您有一条(新)工单到达，请及时处理！(一般)-业务类型为：-(业务办理)--(用检业务)--(现场用电检查)-，工作单号为：(SL070020260820476027)，工单派工时间为：(2026-08-20 09:01:06)，地址为：(一区佳埇队)，联系人为：(客户)，联系方式为：(手机)，联系内容为：(18976509758)，诉求内容为：(客户来电反映南网的空气开关烧坏了，要求我司派工作人员处理，请尽快联系客户核实处理。)，用户编号：(0708003000020093)，用户名称：(吴清美)"

// 变体: 无【南方电网】前缀的转发
const sampleNoPrefix = "尊敬的用户：您有一条[新]工单到达，请及时处理！[一般]-业务类型为：-[故障报修]--[低压停电]--[一带停电]--[其它原因]，工作单号为：[SL070020260721149642]，工单派工时间为：[2026-07-21 08:34:19]，地址为：[海南省省直辖县级行政区定安县龙湖镇安仁村委会高钗村]，联系人为：[先生]，联系方式为：[手机]，联系内容为：[13005016188]，诉求内容为：[客户来电反映上址一带停电，自检为有线路断线，要求工作人员尽快联系处理]，用户编号：[]，用户名称：[]"

// 变体: 【海南电网】前缀
const sampleHainanPrefix = "【海南电网】尊敬的用户：您有一条【新】工单到达，请及时处理！【紧急】-业务类型为：-【故障报修】--【低压停电】--【一户停电】--【线路故障】，工作单号为：【SL070020260728461955】，工单派工时间为：【2026-07-28 14:00:57】，地址为：【海南省省直辖县级行政区定安县龙湖镇居丁村委会丁丰路24号】，联系人为：【吴祖桦】，联系方式为：【手机】，联系内容为：【13647539088】，诉求内容为：【客户反映一户无电，请尽快联系客户核实处理。】，用户编号：【0708030106295166】，用户名称：【吴祖桦】"

// 变体: 配网故障抢修管理平台的报障单(KF 单号)
const sampleKF = "【南方电网】【电网管理平台-配网故障抢修管理】 您有一条新的报障单，请及时处理！报障单号为：【KF2026080117375886068】，故障单发生时间：【2026-08-01 17:33:20】，故障地址：【海南省省直辖县级行政区定安县黄竹镇昌盛街155号】，报障描述：【【客户要求加急】客户反映一户停电，95598已引导客户检查，自查不出原因，经核查系统无停电记录，请尽快联系客户核实处理。 】， 联系电话：【18876701648】。"

func TestPrefixVariants(t *testing.T) {
	r := testRegistry(t)

	orders, err := r.Extract(msg(sampleNoPrefix))
	if err != nil || len(orders) != 1 {
		t.Fatalf("no-prefix: %v %d", err, len(orders))
	}
	if orders[0].OrderNo != "SL070020260721149642" || orders[0].Category != "故障报修/低压停电/一带停电/其它原因" {
		t.Errorf("no-prefix wrong: %+v", orders[0])
	}

	orders, err = r.Extract(msg(sampleHainanPrefix))
	if err != nil || len(orders) != 1 {
		t.Fatalf("hainan-prefix: %v %d", err, len(orders))
	}
	if orders[0].OrderNo != "SL070020260728461955" || orders[0].Priority != "紧急" {
		t.Errorf("hainan-prefix wrong: %+v", orders[0])
	}
}

func TestFormatEKF(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleKF))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.Format != "csg-e-报障单" {
		t.Errorf("format = %q", wo.Format)
	}
	if wo.OrderNo != "KF2026080117375886068" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	wantTime := time.Date(2026, 8, 1, 17, 33, 20, 0, time.Local).Unix()
	if wo.DispatchTime != wantTime {
		t.Errorf("dispatch_time = %d, want %d", wo.DispatchTime, wantTime)
	}
	if wo.ContactPhone != "18876701648" {
		t.Errorf("contact_phone = %q", wo.ContactPhone)
	}
	if !strings.Contains(wo.Description, "要求加急") {
		t.Errorf("description = %q", wo.Description)
	}
}

func TestFormatDFullWidthParens(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg(sampleDParen))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("got %d orders, want 1", len(orders))
	}
	wo := orders[0]
	if wo.Format != "csg-d-工作单(圆括号)" {
		t.Errorf("format = %q", wo.Format)
	}
	if wo.OrderNo != "SL070020260820476027" {
		t.Errorf("order_no = %q", wo.OrderNo)
	}
	if wo.Priority != "一般" {
		t.Errorf("priority = %q", wo.Priority)
	}
	if wo.Category != "业务办理/用检业务/现场用电检查" {
		t.Errorf("category = %q", wo.Category)
	}
	if wo.ContactWay != "手机" || wo.ContactPhone != "18976509758" {
		t.Errorf("contact = %q/%q", wo.ContactWay, wo.ContactPhone)
	}
	if wo.UserName != "吴清美" || wo.UserNo != "0708003000020093" {
		t.Errorf("user = %q/%q", wo.UserName, wo.UserNo)
	}
	if !strings.Contains(wo.Description, "空气开关烧坏") {
		t.Errorf("description = %q", wo.Description)
	}
}

// 每条样例必须只命中一个格式: 签名含括号风格,不产生跨格式误报
func TestEachSampleMatchesExactlyOneFormat(t *testing.T) {
	r := testRegistry(t)
	for _, s := range []string{sampleA, sampleB, sampleBNested, sampleC, sampleBSingleCat, sampleBLeadingNest, sampleDParen, sampleNoPrefix, sampleHainanPrefix, sampleKF} {
		matched := 0
		for _, ex := range r.Extractors() {
			if ex.Match(s) {
				matched++
			}
		}
		if matched != 1 {
			t.Errorf("sample matched %d formats, want 1: %.30s...", matched, s)
		}
	}
}

func TestOrdinaryMessageIgnored(t *testing.T) {
	orders, err := testRegistry(t).Extract(msg("今天晚上吃什么？"))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 0 {
		t.Fatalf("got %d orders, want 0", len(orders))
	}
}

func TestSignatureHitWithoutOrderNoIsError(t *testing.T) {
	// 签名命中但工单号为空(结构漂移): 必须报错,不能静默丢单
	broken := "【南方电网】尊敬的用户：您有一条[新]工单到达，工作单号为：[]，地址为：[海口市]"
	_, err := testRegistry(t).Extract(msg(broken))
	if err == nil {
		t.Fatal("expected error for signature hit without order number")
	}
}

func TestDescriptionMayContainCommas(t *testing.T) {
	// 值内部的中文标点不能干扰括号切分(样例本身已覆盖,这里再断言一次)
	orders, err := testRegistry(t).Extract(msg(sampleA))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(orders[0].Description, "联系人") {
		t.Errorf("description leaked into next field: %q", orders[0].Description)
	}
}

func TestUnclosedBracketStopsParsing(t *testing.T) {
	broken := "【南方电网】【电网管理平台】故障单号为：【DY123，工单派工时间为：【2026-08-02 18:29"
	r := testRegistry(t)
	// 未闭合的括号导致解析不出工单号 -> 报错而非 panic 或静默
	if _, err := r.Extract(msg(broken)); err == nil {
		t.Fatal("expected error for unclosed bracket")
	}
}

func TestDisabledFormatSkipped(t *testing.T) {
	cfgs := DefaultFormats()
	cfgs[0].Enabled = false
	r := NewRegistry()
	if err := r.Load(cfgs); err != nil {
		t.Fatal(err)
	}
	orders, err := r.Extract(msg(sampleA))
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 0 {
		t.Fatalf("disabled format produced %d orders", len(orders))
	}
	// 格式B仍然工作
	if orders, err = r.Extract(msg(sampleB)); err != nil || len(orders) != 1 {
		t.Fatalf("format B broken by disabling A: %v %d", err, len(orders))
	}
}

func TestBadSignatureAbortsLoad(t *testing.T) {
	cfgs := DefaultFormats()
	cfgs[0].Signature = "(unclosed"
	r := NewRegistry()
	if err := r.Load(cfgs); err == nil {
		t.Fatal("expected load error for bad signature regex")
	}
	if r.Count() != 0 {
		t.Fatal("broken load must not replace active extractors")
	}
}

func TestAliasRoundTrip(t *testing.T) {
	m := ParseAliases("故障单号=order_no\n# 注释\n\n地址 = address\ninvalid-line\n")
	if m["故障单号"] != "order_no" || m["地址"] != "address" || len(m) != 2 {
		t.Fatalf("ParseAliases: %v", m)
	}
	s := FormatAliases(m, []string{"故障单号"})
	if !strings.Contains(s, "故障单号=order_no") || !strings.Contains(s, "地址=address") {
		t.Fatalf("FormatAliases: %q", s)
	}
}

func TestParseBracketKVKeys(t *testing.T) {
	pairs := parseBracketKV(sampleA, '【', '】')
	got := map[string]string{}
	for _, p := range pairs {
		got[p.Key] = p.Value
	}
	for _, k := range []string{"故障单号", "工单派工时间", "故障地址", "报障描述", "联系人", "联系电话"} {
		if got[k] == "" {
			t.Errorf("key %q missing, got %v", k, got)
		}
	}
	if _, junk := got["您有一条"]; junk {
		t.Errorf("prefix tokens should be skipped: %v", got)
	}
}
