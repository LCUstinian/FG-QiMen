# 安全策略

> [English](SECURITY.md)

GitHub 在安全通告流程中识别**仓库根目录**的 `SECURITY.md`。完整安全模型见
[`docs/SECURITY.zh-CN.md`](docs/SECURITY.zh-CN.md)；本文件是 **GitHub 要求**的
政策文件。

## 支持的版本

| 版本 | 支持 |
|---------|-----------|
| 最新发布版 | ✅ |
| 较旧次版本 | 尽力而为 |
| v0.2.x 及更早 | 生命周期结束 |

## 上报漏洞

**请开 GitHub Security Advisory**（私密披露）：

👉 [https://github.com/LCUstinian/FG-QiMen/security/advisories/new](https://github.com/LCUstinian/FG-QiMen/security/advisories/new)

或直接邮件维护者（如有 `.github/CODEOWNERS` 可查）。**安全敏感发现请勿开公开
issue。**

### 应包含的内容

1. **受影响版本**（commit SHA 或发布 tag）。
2. **受影响组件**（插件、core、output 等）。
3. **复现步骤**——最小命令 + 输入。
4. **影响**（RCE / 凭据泄露 / DoS 等）。
5. **建议修复**（可选但欢迎）。

### 响应 SLA

| 严重度 | 确认 | 补丁 |
|----------|-----------------|-------|
| **Critical**（RCE / 认证绕过） | 24 小时 | 7 天 |
| **High**（凭据泄露 / 提权） | 3 天 | 30 天 |
| **Medium**（DoS / 信息泄露） | 7 天 | 90 天 |
| **Low**（外观 / 加固） | 14 天 | 尽力而为 |

## 安全模型（TL;DR）

完整细节见 [`docs/SECURITY.zh-CN.md`](docs/SECURITY.zh-CN.md)。三条 HARD 规则：

- **无认证后动作。** 凭据测试把 `user/pass` 写入 `creds.txt` 后停止。
  没有 `ssh.NewSession`、没有 `Exec`、没有 shell。
- **无 CVE 利用。** 没有 EternalBlue、SMBGhost、Redis RCE、JDWP、
  RMI、WebLogic 反序列化。
- **无反弹/正向/SOCKS5 服务端。** FG-QiMen 是*客户端*。

由每个 authenticator 的 `// HARD:` 注释、一条 grep `ssh.NewSession` /
`.Shell(` / `os/exec`（凭据 + 插件路径）的 CI lint、以及 cosign 签名 +
SBOM 附件的发布物共同强制。

## 加密（TL;DR）

设置 `FG_QIMEN_PROJECT_KEY` 后，项目 DB
（`fgqm_workspace/projects/<name>/fgqm.db`）静态存储为 AES-256-GCM。密钥派生：
v0.3.x 用 SHA-256（遗留，仍可读）；v0.4+ 用 OWASP-2024 参数的 Argon2id。
magic 字节 AAD 绑定防止位翻转 → "明文"混淆。完整磁盘格式与 KDF 分发见
[`docs/SECURITY.zh-CN.md`](docs/SECURITY.zh-CN.md) 与 `internal/store/crypto.go`。

## 范围外

- 需要 root / raw socket 的网络扫描器（FG-QiMen 只用 `net.Dial`——
  不发原始包）。
- 针对 FG-QiMen 本体的二进制利用（项目是单静态二进制；攻击面是
  bbolt 文件、配置文件与环境变量——全部受 HARD 规则约束）。
