# 配置

> [English](CONFIGURATION.md)

如何配置 FG-QiMen——flag 分区、环境变量、字典文件与按工作流的示例。

## Flag 查找

```bash
fg-qimen --help                # 按 category 分组的全部 flag
fg-qimen scan --help           # 子命令专属
```

**完整 flag 表**——每个 flag 的短别名、默认值与描述——在
[FLAGS.md](FLAGS.md)。它从 `fg-qimen --help` 使用的同一份 pflag registry
生成，永不漂移；漂移时 CI 守卫测试变红。本页只讲分区、环境变量与
可复制的示例。

短参遵循 v0.5.1 约定（见[更新日志](../CHANGELOG.zh-CN.md)）：全小写、
助记符式，唯一概念用 1 字母（`-H`、`-f`、`-a`、`-r`、`-t`、`-u`、
`-p`、`-v`），命名空间用 2 字母（`-ot`、`-oj`、`-oc`、`-uf`、`-pf`）；
唯一大写是 `-H`（避开 cobra 保留的 `-h`/`--help` 冲突）。

## Flag 分区

| 分组 | 覆盖内容 |
|-------|--------|
| Target | 主机：内联 / 文件 / 排除（CIDR、范围、RFC1918 快捷方式） |
| Workspace | 项目名、DB 密钥、运行模式、恢复、`--workspace` 根目录 |
| Ports | 端口规格、排除、可选 UDP 探测（`--udp` / `--udp-strict`）、仅存活 |
| Network | HTTP 代理、SOCKS5、网卡、按协议超时、Web 指纹规则集 |
| Concurrency | 线程上限、超时策略、退出预算、插件 worker 上限 |
| Credentials | 内联 user/pass、字典文件、HTTP 表单爆破 |
| Output | txt / NDJSON / CSV / SARIF sink、轮转、存活清单格式 |
| Schedule | `--at` / `--in` / `--cron` / `--tz` / `--daemon` / dry-run |
| Behavior | silent、TUI、批量模式、ICMP、预筛、详细度、插件过滤 |
| Safety | 凭据脱敏、insecure-TLS/SSH 退出项、known_hosts |

## 环境变量

| 变量 | 效果 |
|----------|--------|
| `FG_QIMEN_PROJECT_KEY` | 启用 bbolt 值的 AES-256-GCM 加密。生成密钥材料：`openssl rand -hex 32`。 |
| `FGQI_WORKSPACE` | 默认工作区根（未设置时 `./fgqm_workspace`）；`--workspace` flag 优先。 |
| `FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1` | 让 `--output-txt`/`--output-json`/`--output-csv` 跳过项目根路径约束检查。 |
| `FG_QIMEN_SOCKS5_USER` / `FG_QIMEN_SOCKS5_PASS` | SOCKS5 代理认证（CLI flag 值优先）。 |
| `NO_COLOR` | 关闭 ANSI 颜色（遵循 https://no-color.org/）。文本 banner 与 TUI 均遵守。 |
| `TERM=dumb` | 跳过 TUI。 |
| `CI` 系列 | 跳过 TUI。 |

## 字典文件

`--user-file`（每行一个用户名）：

```
admin
root
test
oracle
postgres
```

`--pass-file`（每行一个密码；`#` 行跳过）：

```
# top-10 worst passwords
123456
password
admin
root
qwerty
```

空行忽略。文件在扫描开始时一次性读取。

## 示例

```bash
# /24 网段即扫即走，默认端口
fg-qimen -H 192.168.1.0/24

# 项目模式 + 加密
export FG_QIMEN_PROJECT_KEY=$(openssl rand -hex 32)
fg-qimen --project corp -H 10.0.0.0/24 --mode linked \
    -u root,admin -p 123456,admin

# 中断后恢复
fg-qimen resume --project corp

# 走 SOCKS5 代理
fg-qimen -H 10.0.0.0/24 --socks5 socks5://127.0.0.1:1080

# 按协议超时
fg-qimen -H 10.0.0.0/24 --timeout 5s --web-timeout 10s

# 可选 UDP 服务探测（防火墙网段加 --udp-strict）
fg-qimen -H 10.0.0.0/24 --udp

# 输出到别的目录（选择性加入的路径）
FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1 fg-qimen -H 10.0.0.0/24 \
    -ot /tmp/scan.txt -oj /tmp/scan.json

# 排一个跨时区扫描在指定瞬时启动（时间戳的 offset 就是时区——不用 --tz）。
fg-qimen --project corp -H 10.0.0.0/24 --at "2026-12-25T09:00:00+08:00"

# 每天上海时间 9am 跑 cron 调度扫描。Ctrl-C 退出；--schedule-dry-run 不等、只打预览。
fg-qimen --project corp -H 10.0.0.0/24 --cron "0 9 * * *" \
    --tz Asia/Shanghai --daemon

# 调度持久化到项目 DB（重启后保留），查看挂起的调度。
fg-qimen schedules add    --project corp morning --cron "0 9 * * *"
fg-qimen schedules list  --project corp
fg-qimen schedules remove --project corp morning
```

## 另见

- `README.zh-CN.md` — 面向用户的总览
- [FLAGS.md](FLAGS.md) — 生成的完整 flag 表（单一事实源）
- [PLUGINS.md](PLUGINS.md) — 生成的插件名册
- `docs/ARCHITECTURE.zh-CN.md` — 设计原理
- `docs/SECURITY.zh-CN.md` — 威胁模型 + 加密
- `docs/PLUGIN_GUIDE.zh-CN.md` — 新增插件
