# 安全模型

> [English](SECURITY.md)

FG-QiMen 是扫描器 + 凭据测试器，不是攻击工具。本文档是项目与操作者之间的契约。

## 威胁模型

**在范围内**：
- 授权的内部网络侦察（资产发现、弱口令检测、端口清点）。
- 发现本应被防火墙隔离的暴露服务。
- 审计友好的凭据喷洒：记录命中但**不做任何认证后动作**（不执行命令、
  不写文件、不留后门）。

**范围外**（代码中明确不存在）：
- 任何漏洞利用（CVE RCE、反序列化、认证绕过）。
- 持久化 / 后门 / 横向移动。
- 凭据成功后的自动化（在认证成功的主机上执行命令、投放 WebShell、
  写 SSH key 等）。

## HARD 规则

项目通过代码评审与每个 authenticator 文件顶部的 `// HARD:` 注释强制
以下不变量：

1. **无认证后动作。** `Credential()` 返回命中时，管线把 `user / pass`
   写入 `creds.txt` 然后**停止**。没有 `ssh.NewSession`、没有 `Exec`、
   没有 `Shell`、没有 WebShell、没有文件写入——代码里没有，也不提供
   可配置选项。

2. **无 CVE 利用。** 没有 EternalBlue、SMBGhost、Redis RCE、JDWP、
   RMI、反序列化攻击。代码不含这些路径，我们也不会合并它们。

3. **无反弹 / 正向 / SOCKS5 服务端。** FG-QiMen 是服务的*客户端*，
   不是给操作者提供立足点的服务端。

## 永远不会包含

以下内容从 v0.1 起就被所有未来版本明确排除：

- ❌ MS17-010（永恒之蓝）探测与利用
- ❌ SMBGhost（CVE-2020-0796）
- ❌ Redis 写公钥 / 写计划任务 / 写 WebShell / 主从复制 RCE
- ❌ SSH 认证后自动执行命令（代码中**不存在** `ssh.NewSession` /
  `Exec` / `Shell`）
- ❌ MS17-010 ShellCode 注入 / SMB ShellCode
- ❌ JDWP 利用
- ❌ RMI / JBoss / WebLogic 反序列化 RCE
- ❌ 任何 CVE-based 的远程代码执行
- ❌ 反弹 Shell / 正向 Shell / SOCKS5 代理服务端（后渗透）
- ❌ 凭据成功后的任何自动化操作（写文件、执行命令、植入后门）

### 爆破的严格定义

✅ **允许**：用 user:pass 字典对 SSH / RDP / FTP / MySQL / Redis /
SMB 等服务做标准认证握手尝试。

✅ **命中时**：把 `user / pass` 写入 `creds.txt` 然后停止。插件函数
返回带 `Cred` 字段的 `*Result`；管线写盘后即终止；不调用
`Session.Exec`、不上 WebShell、不执行任何命令。

❌ **严禁**：任何认证后动作——执行远程命令、写远程文件、植入持久化等。

**扫描器 + 凭据测试器 = 探测面工具。漏洞利用 = 攻击面工具。
FG-QiMen 只做前者。**

## 静态加密

项目模式把结果持久化到 `fgqm_workspace/projects/<name>/fgqm.db` 的
bbolt DB。默认明文写入（seen-set 桶永远是明文——它只存非机密的
SHA-1 哈希）。

启用加密，设置 `FG_QIMEN_PROJECT_KEY`：

```bash
# 生成强密钥：
export FG_QIMEN_PROJECT_KEY=$(openssl rand -hex 32)

# 运行扫描；新写入经 AES-256-GCM 加密：
fg-qimen --project myproject -H 10.0.0.0/24 --mode linked
```

密钥派生：
  - v0.3.x（遗留，仍可读）：SHA-256(passphrase) → 32 字节密钥。
  - v0.4+（现行，新写入）：Argon2id(passphrase, salt)，OWASP-2024
    参数（time=3, memory=64 MiB, parallelism=4, salt=16 B）。salt 每
    DB 独立并缓存在 `EncryptedValue`，成本只在项目打开时支付一次。

磁盘格式记录在 `internal/store/crypto.go`：

```
+--------+------------------+--------------------+
| magic  |      nonce       | ciphertext + tag   |
| 1 byte |  12 bytes (GCM)  |  N bytes           |
+--------+------------------+--------------------+
```

- `0x00` magic：明文（遗留 v0.2.x）
- `0x01` magic：v0.3.0 加密，SHA-256 密钥，AAD=nil（遗留）
- `0x02` magic：v0.3.1+ 加密，SHA-256 密钥，AAD=magic（遗留，可读）
- `0x03` magic：v0.4+ 加密，Argon2id 密钥，AAD=magic（现行）

`Open()` 按 magic 字节分发到正确的 KDF，所以 v0.3.x 的 DB 在 v0.4+
构建上仍可读；设置 `FG_QIMEN_PROJECT_KEY` 后新写入总是用 `0x03`。

magic 字节经 GCM AAD 与密文绑定。对 magic 字节的位翻转会被检测为
`ErrDecryptFailed`，而不是把加密行静默当"明文"（审计路线图 P1.4）。

## 凭据脱敏

- `fgqm_creds.txt` **永远是明文**——这是操作员的工作文件。
- `fgqm_result.txt`、`fgqm_result.ndjson`、`fgqm_result.csv` 默认脱敏为
  仅长度指纹（如 `admin / ******** (len=8)`）。传 `--show-creds` 可让
  这些文件也嵌入明文。
- TUI / stderr 遵循同一脱敏门控。

## 输入校验

- 主机经 `types.ExpandTargets` 展开，校验 CIDR / 范围 / 主机名语法。
- `--host` 的值不经 shell 求值；子进程（系统 `ping`、`arp`）以
  `exec.Command` 调用，命令注入不可能。
- 以 `-` 开头的主机值**被拒绝**（系统 `ping` 会把它当 flag 解析）。
  审计路线图 P1.9。
- 工作区项目名经 `workspace.ValidateProjectName`（允许列表，无 `..`、
  无绝对路径）。
- 输出路径经 `cmd/scan.go:safeOutputPath`，强制项目根边界，除非设置
  `FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1`。

## SSH host key 验证

默认情况下，SSH 凭据喷洒要求已填充的 `known_hosts` 文件。flag 为
`--known-hosts <path>`。flag 与默认文件都不可用时，喷洒**拒绝运行**
并向 stderr 输出明确错误。

选择退出（生产环境不建议）：

```bash
fg-qimen --project myproject --insecure-ssh -H 10.0.0.0/24 --mode linked
```

## TLS 验证

HTTPS 探测默认验证证书链 + 主机名。flag `--insecure-tls` 为已知可信
的自签名测试环境关闭验证。

## 已知限制

- **无 SSRF / 回环防护。** 对 `127.0.0.0/8` 或 `169.254.0.0/16`
  （link-local）的扫描完全放行。这是设计使然——操作员测试时会刻意
  扫描回环。未来的 `--no-loopback` flag 可加选择加入的警告。
- **无 DNS-rebinding 防护。** 解析与连接之间解析到不同 IP 的目标会在
  第二个 IP 上被扫描。标准 Go `net.Resolver` 行为；非扫描器特有问题。
- **明文的内存暴露。** 凭据字符串存在于 Go 托管的堆内存；GC 前的进程
  内存转储可恢复它们。缓解：传 `--insecure-ssh=false`、
  `--no-batch=false`、设置 `FG_QIMEN_PROJECT_KEY`，共享多用户主机上
  优先文件字典而非内联 `-u`/`-p`。

## 漏洞上报

请开 GitHub Security Advisory（私密披露）。安全敏感发现不要开公开
issue。
