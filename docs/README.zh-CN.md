# FG-QiMen 文档

> [English](README.md)

这是 FG-QiMen 用户文档与开发者文档的权威索引。仓库内布局：

```
docs/
├── README.md                    ← 中文版即本文件
│
├── ARCHITECTURE.md              ← 管线设计、插件契约、session bag
├── ARCHITECTURE.zh-CN.md
├── CONFIGURATION.md             ← CLI flag 分区 + 用法模板（全表见 FLAGS.md）
├── CONFIGURATION.zh-CN.md
├── FLAGS.md                     ← 生成物：从活体 registry 渲染的完整 flag 表
├── FLAGS.zh-CN.md               ← 生成物：FLAGS.md 的中文孪生版
├── PLUGINS.md                   ← 生成物：从活体 registry 渲染的完整插件名册
├── PLUGINS.zh-CN.md             ← 生成物：PLUGINS.md 的中文孪生版
├── PLUGIN_GUIDE.md              ← 如何编写新插件 / authenticator
├── PLUGIN_GUIDE.zh-CN.md
├── SECURITY.md                  ← HARD 规则 + 无漏洞利用契约
├── SECURITY.zh-CN.md
├── RELEASE.md                   ← 如何发版（tag → 多平台）
└── RELEASE.zh-CN.md
```

每份文档都是英文/简体中文成对存在（`*.md` / `*.zh-CN.md`），结构保持
同步。两个 `生成物` 是 `just docs-gen` 的构建产物——绝不手改；一旦与
活体 flag/插件 registry 漂移，CI 守卫测试（internal/docgen）立刻变红。

## 面向用户的文档（顶层）

这些文档从项目根 [README.zh-CN.md](../README.zh-CN.md) 链接，描述工具
是什么、怎么用。

| 文件 | 用途 |
|---|---|
| [FLAGS.md](FLAGS.md) / [FLAGS.zh-CN.md](FLAGS.zh-CN.md) | **生成物**：完整 flag 表——每组一节，从 `fg-qimen --help` 使用的同一份 pflag registry 渲染。用 `just docs-gen` 重新生成；漂移时 CI 守卫测试变红，且每个 flag 必须配中文说明。 |
| [PLUGINS.md](PLUGINS.md) / [PLUGINS.zh-CN.md](PLUGINS.zh-CN.md) | **生成物**：插件名册——名称、默认端口、Identify/Credential 能力，来自活体插件 + authenticator registry。与 FLAGS.md 同一守卫。 |
| [ARCHITECTURE.zh-CN.md](ARCHITECTURE.zh-CN.md) | 管线设计（alive → scan → identify → spray 四阶段）、插件契约（Identify + Credential 双模式接口）、session bag 接线、bbolt 项目工作区 |
| [CONFIGURATION.zh-CN.md](CONFIGURATION.zh-CN.md) | flag 分区（Target / Workspace / Ports / Network / Concurrency / Credentials / Output / Schedule / Behavior / Safety）+ 按工作流的用法模板 |
| [PLUGIN_GUIDE.zh-CN.md](PLUGIN_GUIDE.zh-CN.md) | 如何编写新插件或 authenticator；`Plugin` 接口契约；如何注册 Identify / Credential 模式 |
| [SECURITY.zh-CN.md](SECURITY.zh-CN.md) | HARD 无漏洞利用规则；禁止能力清单；审计驱动的安全姿态；SSH host-key 处理；凭据仅内存存储 |
| [RELEASE.zh-CN.md](RELEASE.zh-CN.md) | 发版前清单；tag 触发的发布管线；11 平台矩阵；SHA256SUMS、cosign、SBOM 的产生方式 |

## 内部工作文档（仅本地）

`docs/` 下有几个目录**不入库**（已 gitignore），因此不在此链接：
`verification/`（按版本的验证与基准报告）、`design/`（带日期戳的设计
规格）、`superpowers/`（agent 会话的迭代计划）、`archive/`（历史审计
报告与进度日志）。它们只留在贡献者本机和 CHANGELOG 的纯文本路径引用
里；需要某份报告时找维护者要，或从 CHANGELOG 条目重建——每个条目都
概述了对应报告的结论。
