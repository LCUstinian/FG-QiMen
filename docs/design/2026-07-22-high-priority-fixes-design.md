# 第一批高优先级修复设计

- 日期：2026-07-22
- 范围：发布完整性、配置语义、错误传播、并发取消与验证
- 实施策略：最小局部修复 + 测试先行验收

## 1. 目标与非目标

### 目标

1. 发布流程在产物缺失、checksum 为空或不完整时 fail-closed，并验证至少一个原生构建产物可运行。
2. 让高风险 CLI 配置真正影响生产路径：字典文件、排除端口、no-state 和端口规格解析错误。
3. 消除已定位的共享状态竞态，并让预筛选在 Context 取消后及时停止。
4. 为每一项修复添加可重复的单元或命令级集成测试。
5. 通过 `go test ./...`、`go test -race ./...` 和 `go vet ./...`。

### 非目标

本批不重构完整运行时配置体系，不实现完整 crack mode 重构，不迁移所有插件到统一 proxy，不并行化结果 sink，也不进行未经 benchmark 证明的性能优化。

## 2. 设计

### 2.1 发布 Gate

修改 release workflow：

- 显式定义五个预期二进制文件，而非依赖宽泛 glob。
- 对每个文件执行存在性检查。
- 在严格 shell 模式下生成 `SHA256SUMS`。
- 断言 checksum 条目数量和文件内容非空。
- 对生成的 checksum 执行校验。
- 在 Linux 原生 runner 上执行 `version` 和 `--help`，并检查版本与 tag 一致。

发布权限和 action pinning 暂不纳入本批生产改动，作为后续 release hardening 任务。

### 2.2 配置解析与扫描输入

在启动 goroutine 和扫描阶段前完成同步解析：

- 通过现有 credential loader 合并 inline/file 用户名和密码，并传播读取及限制错误。
- 解析 include/exclude ports，生成去重后的有效端口集合。
- malformed/out-of-range port specification 直接从 `RunScan` 返回错误。
- `NoState` 在 session/workspace 构建阶段阻止 Store 和 BatchWriter 创建。
- 保持现有 `Config` 和 Session 结构，避免本批引入新的全局 RuntimeConfig 类型。

所有配置错误都必须在产生网络任务前返回，不能在 producer goroutine 中只记录日志后成功结束。

### 2.3 并发与取消

- `cmdProbe` 的有效 timeout 改为 `Probe` 内局部变量，移除并发写共享字段。
- `RetryStats` 的并发计数改为 atomic 或在现有结构上增加同步保护；对外统计 API 保持兼容。
- prescreen producer 使用 labeled break 退出 task 循环。
- semaphore acquire 使用同时监听 `ctx.Done()` 的 select。
- 不改变现有 worker 数量、channel 容量和扫描调度策略。

### 2.4 错误传播边界

本批重点处理配置/输入错误和已定位的并发错误。输出、Store、BatchWriter 全量错误传播属于同一主题但涉及更多接口，设计上保留为下一小批；本批至少不扩大 `_ =` 忽略错误的范围，并为后续改造保留清晰边界。

## 3. 测试策略

### 单元测试

- credential file-only、inline+file、不可读文件和超限。
- include/exclude port 合并、去重、空集和非法规格。
- `NoState` 不创建 Store/BatchWriter。
- cmd probe timeout 并发调用。
- RetryStats 并发计数。
- prescreen Context 取消时不继续创建任务。

### 命令/集成测试

- 使用临时目录验证 project + no-state 不生成 `fg.db`。
- 使用 fake probe 或测试扫描器验证排除端口不进入 probe。
- 验证无效端口规格返回非零错误，而不是成功空扫描。
- 验证 file-only credentials 进入认证阶段。

### 验证命令

```text
go test ./...
go test -race ./...
go vet ./...
```

如 release workflow 有 YAML 或 shell 校验脚本，额外执行对应的静态验证；不在本地伪造 GitHub release。

## 4. 兼容性与回滚

- 保持现有 CLI 参数名称和成功路径不变。
- 错误输入从“静默成功”变为非零失败，这是有意的行为修正。
- `--no-state` 在项目模式下不再创建数据库，这是公开语义的兑现。
- 代码改动按主题分组，任何单个主题均可独立回滚。
- 不改变协议认证行为和网络探测默认并发度。

## 5. 验收标准

第一批完成必须同时满足：

1. 所有新增回归测试通过。
2. `go test ./...` 通过。
3. `go test -race ./...` 通过，或对环境限制给出明确记录。
4. `go vet ./...` 通过。
5. 公开配置的失败输入不再产生成功空扫描。
6. release checksum 生成不再使用静默成功兜底。
7. 工作区 diff 只包含本批设计和实现范围，没有无关格式化或重构。
