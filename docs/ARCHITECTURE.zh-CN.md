# 架构

> [English version](../ARCHITECTURE.md)

## 项目范围

**In scope：** 纯扫描器 + 凭证测试器。授权内网侦察、资产发现、弱口令检测、
端口清点。

**Out of scope（设计层面永远不做）：**

- 漏洞利用（CVE RCE、反序列化、鉴权绕过）。
- 持久化 / 后门 / 横向移动。
- 命中凭证后的自动化操作（在目标上跑命令、丢 webshell、写 SSH key）。
- 与其他工具重复的 exploit-framework 功能（Metasploit / Sliver / Cobalt
  Strike 都做得更好；FG-QiMen 故意不跟他们竞争）。

详细 rationale 在 [`docs/SECURITY.md`](../SECURITY.zh-CN.md)（threat model
+ HARD 规则）。README 的 tagline（"功能多 ≠ 好"）是一句话版本。

## 流水线数据流

```
hostiter ─ch(host)──> portscan ─ch("host:port")──> pluginWorker
                                                       │
                                                       ├─ Identify plugin ─┐
                                                       └─ Credential plugin┴─> sink
sink = output（TXT + NDJSON + creds）+ store（bbolt 去重）
```

每个阶段是一个 goroutine。Context 取消通过每个 channel 传播。

## 为什么 channel 解耦？

端口扫描是最快的阶段。plugin worker 是最慢的。解耦让 scanner 在慢
worker 啃一个 target 的同时把 buffer 填满。

Channel buffer `DefaultChannelBuffer = 1024` —— 足以应付 200 worker
的突发，小到 SIGINT 能在 shutdown-timeout 窗口内排空。

## Stage 生命周期 + 视图投影

Scanner 通过 `internal/types.State` 上的一个小状态机上报进度，让 UI
不用窥探 scanner 内部：

- **`Stage` 枚举**（`internal/types/state.go`）：`int32`，命名常量
  `StageNone → StageAlive → StagePortScan → StageIdentify → StageCred
  → StageDone`。Scanner 在每个阶段开始 / 结束时推进 `State.Stage`。
  TUI header 的 `[ ▶ STAGE ]` 徽章直接读这个字段渲染。
- **`PluginHits` map**（`State.PluginHits[pluginID]int64`）：每个
  plugin 的命中计数，由 plugin worker 触发时填充。供 TUI 的
  "top plugins" 条形图使用。
- **`ErrorCategories` map**（`State.ErrorCategories[category]int64`）：
  按错误类别（`timeout`、`refused`、`dns` …）的计数，由
  `core.ClassifyError` 填充。
- **`CountersView` 投影**（`State.CountersView()`）：返回只读快照
  struct，让视图层读稳定契约而不是改共享 map。TUI 一律走这个——
  不直接 touch `PluginHits` 或 `ErrorCategories`。
- **`core.ClassifyError(err)`**（`internal/core/errors.go`）：把扫描
  错误分类到命名桶。查找顺序是先对一组类型化 sentinel 做
  `errors.Is` / `errors.As`，最后才回退到子串匹配；子串步骤是为
  原始 `*net.OpError` / `syscall.ECONNREFUSED` 字符串（不会冒出
  类型化 sentinel）留的安全网。
- **`alive.Progress()`**（`internal/core/alive/cmd.go`）：外部调用
  方观察 mid-alive-sweep 探测数的公共 API，不用耦合到 scanner 的
  channel 布局。TUI 的 "alive N/M" 计数器从这里取数。

为什么这么做：上面 channel 解耦的流水线吞吐好，但对"扫描现在
在干嘛"的可观测性很差。`Stage` 枚举 + `CountersView` 投影就是
桥——让 UI 能报出具体进度，而不用从嘈杂的 per-worker channel 重
新推导状态。

## 包分层

```
cmd/                                Cobra 命令
└── internal/
    ├── session/                    leaf DAG 顶部
    │   ├── types/                  leaf: Config, State, Result, Cred
    │   ├── output/                 多格式 sink
    │   ├── store/                  bbolt 持久化
    │   ├── ui/                     UI 接口 + TextUI + NopUI
    │   └── tui/                    Bubbletea 仪表盘
    ├── core/                       流水线编排
    │   ├── alive/                  主机发现（暴露 Progress() 给
    │   │                          mid-sweep 计数器）
    │   ├── scan/                   端口扫描器（驱动 Stage 生命周
    │   │                          期，填充 PluginHits / ErrorCategories）
    │   ├── credential/             凭证喷射调度
    │   ├── errors/                 ClassifyError(err) → 类别桶
    │   ├── plugins/                Plugin 接口 + 注册表
    │   │   └── adapted/            30 个内置 plugin
    │   ├── portscan/fingerprint/   Nmap PSL 服务指纹
    │   ├── discovery/              仅 LAN 的 ARP + NetBIOS
    │   ├── fakeserver/             适配 plugin 测试用的共享 in-process
    │   │                          test doubles
    │   └── workspace/              ephemeral / project 状态
    ├── scheduler/                  跨时区调度（--at、--in、--cron）；
    │                              cron 解析走 robfig/cron/v3
    └── version/                    ldflag 注入的版本字符串
```

严格向下：`core/` 可以 import `types/`，但 `types/` 不能 import
`core/`。leaf 包不 import 任何 `internal/`。

## Plugin 层

1. **Identify plugins** 在 `internal/plugins/adapted/` 下。返回 banner
   / version / title。
2. **Credential authenticators** 在 `internal/core/credential/auth/<category>/`
   下。讲服务鉴权协议，返回 Hit（或 nil）。

两者都通过 `init()` 自注册。两层都遵守同样的 hard rule：鉴权后不
做后续动作，不做利用。

## 模式

- `scan` —— 只做 Identify。
- `crack` —— 跳过端口扫描，对配置的端口跑 Credential。
- `linked` —— 先跑 scan，再对声明了 `ModeCredential` 的服务触发
  Credential。

## 项目 workspace

```
runs/projects/<name>/
├── fg.db                  # bbolt 状态
├── targets.txt            # 手编目标列表（不加 fgqm_ 前缀——操作员
│                          # 直接编辑）
└── <YYYY-MM-DD>/
    ├── fgqm_result_HH-MM-SS.txt / .json / .csv / .sarif
    ├── fgqm_creds.txt     # 始终明文
    └── fgqm_rdp.json / .txt   # RDP 深度指纹
```

`HH-MM-SS` 后缀（v0.5.1 加入）是本地时间的开始时间戳，scan 启动时
一次性抓取，所以同日两次 run 不会互相覆盖。单次 scan 的所有结果
文件共享同一后缀。通过 `-ot` / `-oj` / `-oc` 传显式路径会跳过
后缀。

`fgqm_` 前缀把每个结果产物标成 fg-qimen 的，所以在混合目录里一眼
能看出来（`ls runs/` → `fgqm_*.txt` 显然是这个扫描器的，不是别的
app）。这个前缀也是稳定的 grep anchor：`grep -l 'fgqm_' runs/`
遍所有项目所有日期找结果文件。`targets.txt` 不加前缀因为那是操
作员需要读和手编的唯一文件。

ephemeral 模式（不带 `-p`）：workspace 是当前目录，没有 bbolt。
project 模式（`-p <name>`）：每个项目独立 bbolt + 可选加密。

## 批量写入（v0.3.1+）

`store.BatchWriter` 累积 `PutOp` 值，每 32 个 ops 或每 200ms flush
一次。`--no-batch` 回退到 per-write 语义。

## 性能

- 自适应 worker pool（filtered / open 比的滑动窗口）。自调到 64-200。
- `LoadSeenHashes` 用 `bk.Stats().KeyN` 预分配，100k 条 resume 避
  免 17 次 log-2 重新分配。
- Output sink 用 6 个 per-sink 互斥锁，慢 sink 不会 head-of-line
  阻塞其他 sink。

## 取舍

- **Pool dedup key 走 HMAC 哈希，但明文还在堆里** —— 进程内存 dump
  可以在 GC 前拿到字符串。已记入 `docs/SECURITY.md`。
- **TUI 默认按需启用。** 非 TTY stdout（CI、脚本）走 text logger。
- **`RawTCPIdentify` 是薄包装** —— 没有抽象所有协议。UDP fallback
  （SNMP）和 TLS probe（HTTPS）还各自写自己的 dial 循环。
