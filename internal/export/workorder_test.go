package export

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"volteye/internal/capture"
	"volteye/internal/store"
)

func seedOrders(t *testing.T, st *store.Store) {
	t.Helper()
	base := time.Date(2026, 8, 3, 8, 41, 9, 0, time.Local).Unix()
	orders := []capture.WorkOrder{
		{ // 4 级分类链 -> 故障报修 sheet, 具体故障并入第 4 级
			OrderNo: "SL070020260803657516", Format: "csg-b-工作单", Priority: "一般",
			Category: "故障报修/低压停电/一户停电/其它原因", DispatchTime: base,
			Address: "定安县", Description: "客户反映上址一户停电",
			ContactName: "先生", ContactWay: "手机", ContactPhone: "13976321900",
			UserNo: "0708003000009775", UserName: "占",
		},
		{ // 2 级分类 -> 咨询意见 sheet
			OrderNo: "07000010000053444552", Format: "csg-b-工作单",
			Category: "意见/其他", DispatchTime: base + 3600,
			Address: "黄竹镇大坡村", Description: "种植槟榔接电",
			ContactName: "先生", ContactWay: "手机", ContactPhone: "19977929959",
		},
		{ // 无优先级无分类(csg-a) -> 未识别到
			OrderNo: "DY2026080218290204", Format: "csg-a-故障单", DispatchTime: base - 3600,
			Address: "定安县", Description: "一户停电",
			ContactName: "女士", ContactPhone: "13876655000",
		},
	}
	if _, err := st.InsertWorkOrders(orders); err != nil {
		t.Fatal(err)
	}
}

func openSheet(t *testing.T, path, sheet string) [][]string {
	t.Helper()
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestWorkOrdersLayoutAndRouting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedOrders(t, st)

	out := filepath.Join(t.TempDir(), "out.xlsx")
	n, err := WorkOrdersXLSX(st, store.WorkOrderFilter{}, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("exported %d rows, want 3", n)
	}

	gz := openSheet(t, out, "故障报修")
	if len(gz) != 4 { // title + meta + header + 1 data row
		t.Fatalf("故障报修 sheet rows = %d, want 4", len(gz))
	}
	if gz[0][0] != "工单汇总表 · 故障报修" {
		t.Fatalf("title wrong: %v", gz[0])
	}
	if gz[2][0] != "紧急程度" || gz[2][4] != "工作单号" || gz[2][13] != "是否完成" {
		t.Fatalf("header mismatch: %v", gz[2])
	}
	r := gz[3]
	if r[0] != "一般" || r[1] != "故障报修" || r[2] != "低压停电" || r[3] != "一户停电/其它原因" {
		t.Fatalf("category split wrong: %v", r[:4])
	}
	if r[4] != "SL070020260803657516" || r[8] != "手机" || r[9] != "13976321900" {
		t.Fatalf("row wrong: %v", r)
	}
	// 联系人是称谓时合并用户名称显示
	if r[7] != "占(先生)" {
		t.Fatalf("contact should merge account name, got %q", r[7])
	}

	zx := openSheet(t, out, "咨询意见")
	if len(zx) != 5 { // 3 scaffolding rows + 2 data rows
		t.Fatalf("咨询意见 sheet rows = %d, want 5", len(zx))
	}
	// 按派工时间升序: 第一行是 csg-a(无分类无优先级), 第二行是 2 级分类的意见单
	if zx[3][0] != "未识别到" || zx[3][1] != "未识别到" {
		t.Fatalf("missing priority/category should be 未识别到: %v", zx[3][:4])
	}
	if zx[4][1] != "意见" || zx[4][2] != "其他" || zx[4][3] != "未识别到" {
		t.Fatalf("2-level split wrong: %v", zx[4][:4])
	}
}

func TestDisplayContact(t *testing.T) {
	cases := []struct {
		contact, user, want string
	}{
		{"先生", "游卫东", "游卫东(先生)"},          // 称谓 + 有户名 -> 合并
		{"客户", "海南福融科技有限公司", "海南福融科技有限公司(客户)"}, // 公司户同理
		{"吴祖桦", "吴祖桦", "吴祖桦"},              // 真名不重复
		{"先生", "", "先生"},                       // 户名空 -> 保留称谓
		{"", "游卫东", "游卫东"},                   // 联系人空 -> 用户名称
		{"", "", ""},                             // 都空
		{"13519865119", "占", "13519865119"},      // 非常称谓原样
	}
	for _, c := range cases {
		got := displayContact(capture.WorkOrder{ContactName: c.contact, UserName: c.user})
		if got != c.want {
			t.Errorf("displayContact(%q, %q) = %q, want %q", c.contact, c.user, got, c.want)
		}
	}
}

func TestWorkOrdersAppendSkipsDuplicates(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedOrders(t, st)

	out := filepath.Join(dir, "out.xlsx")
	if _, err := WorkOrdersXLSX(st, store.WorkOrderFilter{}, out); err != nil {
		t.Fatal(err)
	}

	// 模拟对方手工填写: 是否完成 + 备注
	f, err := excelize.OpenFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("故障报修", "N4", "已解决"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("故障报修", "O4", "已现场处理"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// 新工单到达后追加
	if _, err := st.InsertWorkOrders([]capture.WorkOrder{
		{OrderNo: "SL070020260819444344", Priority: "紧急", Category: "故障报修/线路故障",
			DispatchTime: time.Now().Unix(), Address: "大春村", ContactPhone: "13976320380"},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := AppendWorkOrdersXLSX(st, store.WorkOrderFilter{}, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("append inserted %d rows, want 1 (3 existing skipped)", n)
	}

	gz := openSheet(t, out, "故障报修")
	if len(gz) != 5 { // 3 scaffolding + 2 data rows
		t.Fatalf("故障报修 rows = %d, want 5", len(gz))
	}
	if gz[3][13] != "已解决" || (len(gz[3]) > 14 && gz[3][14] != "已现场处理") {
		t.Fatalf("manual edits lost: %v", gz[3])
	}
	if gz[4][4] != "SL070020260819444344" || gz[4][2] != "线路故障" || gz[4][3] != "未识别到" {
		t.Fatalf("appended row wrong: %v", gz[4])
	}

	// 再追加一次: 全部重复,一行不加
	n, err = AppendWorkOrdersXLSX(st, store.WorkOrderFilter{}, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second append inserted %d rows, want 0", n)
	}
	if gz := openSheet(t, out, "故障报修"); len(gz) != 5 {
		t.Fatalf("rows changed after no-op append: %d", len(gz))
	}
}
