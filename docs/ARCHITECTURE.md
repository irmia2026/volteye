# VoltEye 架构

## 总览

```
微信进程 (Weixin.exe)
   │  内存扫描 / DLL 辅助候选 → 数据库密钥
   ▼
db_storage\ (加密 SQLCipher 库 + WAL)
   │  复制 → 解密(AES-CBC/PBKDF2) → WAL 当前代帧解密回放
   ▼
data\work\dec\  (明文镜像, 仅 1 份)
   │  按 (create_time, sort_seq, local_id) 游标增量读取被监控群的 Msg_<md5> 表
   ▼
data\volteye.db ── messages 表 (全量流水, 原文)
   │  每轮轮询后分发未扫描消息 (scanned=0)
   ▼
┌─────────────┬───────────────────────────┐
│ 规则引擎      │ 格式注册表 Registry          │
│ (关键词/正则)  │ Extractor × N (DB 配置驱动)  │
│ → matched 标记 │ → work_orders / parse_errors │
└─────────────┴───────────────────────────┘
   ▼
TUI 面板 / Excel 导出
```

## 包结构

| 包 | 职责 |
|---|---|
| `cmd/volteye` | 主程序：参数、TUI 启动、托盘模式（关窗 detach、托盘重开） |
| `cmd/volteye-m0` `m1` | 开发验证工具（解密验证 / 采集验证 CLI），勿分发 |
| `internal/wechatdb` | 微信侧一切：目录探测、密钥提取、SQLCipher/WAL 解密、消息读取、内容解码（zstd） |
| `internal/sync` | 采集器：镜像刷新、表发现、游标增量同步、群列表发现、消息分发 |
| `internal/capture` | 结构化抽取：`Extractor` 接口、`bracketkv` 解析器、格式注册表、内置格式 |
| `internal/extract` | 关键词/正则打标引擎（与 capture 并存，负责自定义标记） |
| `internal/store` | 本地 SQLite：messages / work_orders / parse_errors / formats / rules / groups / sync_cursors / settings |
| `internal/export` | Excel 导出：消息流水（旧）+ 生产级工单报表（样式化、分 sheet、追加模式） |
| `internal/tui` | Bubble Tea 面板：总览/群管理/工单/消息流/格式/规则/导出/设置/日志 |
| `internal/app` | Service 层：装配采集器/引擎/注册表，面板与业务之间的门面 |
| `internal/cleanup` | 保留期/容量清理（先归档后删除） |
| `internal/tray` | 托盘图标与控制台 attach/detach |

## 关键设计

### 1. 采集一致性

微信的库被进程独占且持续写入，不能直接读。采集器把变化的分片库复制到 `work/enc`（**临时**）→ 解密到 `work/dec`（常驻镜像）→ 删除 enc。WAL 单独处理：WCDB 的 WAL 是多代际的，解密时按 salt 过滤只保留当前代帧，否则 SQLite 会忽略整个 WAL。

- 主库 sig（size+mtime）变化才重新整库解密；WAL 每轮增量解密
- WAL 解密只需要主库前 16 字节 salt（`ReadSalt`），这是 enc 副本不必常驻的原因
- 解密失败不缓存 sig，下轮自动重试

### 2. 游标与去重

每个 (群, 分片) 一条游标 `(create_time, sort_seq, local_id)`；新群默认跳到最新（不回填），开启回填后从 0 拉全量。消息以 `UNIQUE(group_wxid, src_db, local_id)` 幂等去重，任意时刻重启/重扫都不会产生重复。

### 3. 格式引擎（capture）

目标消息是"括号包裹的键值对"短信。核心认识：**键名清洗 + 括号深度匹配 + 别名归一化** 可以一个解析器覆盖所有这类格式。

- `FormatConfig`（存 `formats` 表）：签名正则、括号对、`源字段=规范字段` 别名表、可选分类链标记
- 规范字段集固定：`order_no dispatch_time address description contact_name contact_way contact_phone category user_no user_name`
- 签名必须锚定"带括号的字段名"（如 `工作单号为：【`），使同短信只命中一个格式（有测试保证）
- 签名命中但解析不出 `order_no` → 记 `parse_errors`（格式漂移可观测）
- 值内嵌套同型括号按深度匹配闭合；深度不归零时回退浅层闭合，避免一个坏值吞掉后续字段

**扩展两个层级**：

1. 配置层：新括号 KV 格式 → TUI 格式面板加一行定义，零代码
2. 代码层：非括号 KV 的消息形态 → 实现接口注册新 kind：

```go
type Extractor interface {
    Name() string
    Kind() string
    Match(content string) bool
    Extract(m Message) ([]WorkOrder, error)
}
```

在 `capture.CompileFormat` 的 switch 里挂新 kind 即可。

### 4. 导出（export）

- 列布局固定 14 列，与供电所习惯一致；分类链按级拆进 业务类型/故障子类/具体故障（第 4 级起并入具体故障，缺省 `未识别到`）
- sheet 路由：`SheetForCategory` 映射（当前 故障报修→同名 sheet，其余→咨询意见），新 sheet 类型加一行映射；`preferredSheetOrder` 固定顺序
- 新文件带完整样式脚手架（标题/元信息/表头/冻结/筛选/列宽）；追加模式按工作单号去重、不重排既有行、不动人工列
- 紧急程度条件色、斑马纹、按内容量估算行高、电话/编号文本格式

### 5. 数据目录探测链

依次尝试，每层都要求候选目录存在 `wxid_*\db_storage\message\message_*.db` 实证：

1. `%APPDATA%\Tencent\xwechat\config\*.ini`（4.x 记录的文件位置父目录）
2. 注册表 `HKCU\Software\Tencent\Weixin / WeChat` 的 `FileSavePath`（3.x/迁移机器）
3. `文档\`、`用户目录\`、各盘符根目录下的 `xwechat_files`（含 `xwechat files` 空格变体）

多账号时按消息库最近修改时间选活跃账号（与运行中进程所登账号一致，保证密钥匹配）。探测失败用 `-dbstorage` 手动指定。

### 6. 生命周期与运维

- 托盘模式：TUI 只是视图，Service 常驻；关窗 detach 控制台，托盘菜单可重开
- 清理策略：`retention_days` / `max_db_mb` 超限时先把数据归档成 xlsx 再删
- seed 版本化：`formats_seed_version` 记录迁移版本，内置格式变更自动升级，用户自建格式不受影响

## 测试策略

- `capture`：全部真实样例逐字段断言 + 每样例恰好命中一个格式 + 边界（嵌套括号/未闭合/空单号/禁用/坏配置）
- `sync`：落库→分发→入表→去重端到端；解析失败记录；解密失败不缓存 sig
- `export`：布局/路由/分类拆分/追加去重/人工列保留/联系人合并
- `tui`：teatest 集成 + 快照布局测试
- `wechatdb`：加解密 round-trip、WAL 代际过滤
