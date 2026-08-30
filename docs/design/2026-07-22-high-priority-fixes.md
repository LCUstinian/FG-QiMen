# 第一批高优先级修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 FG-QiMen 第一批已确认的发布完整性、配置语义、并发取消和竞态问题，并用自动化测试验证行为。

**Architecture:** 保持现有 Cobra → Config → Session → Core Pipeline 架构，不引入新的全局 RuntimeConfig。所有可验证的输入在启动 goroutine 前同步解析；现有 credential loader、Store、Session 和 channel pipeline 继续复用。并发修复采用局部变量、atomic 和可取消 select，不改变 worker 数量、channel 容量或网络默认并发度。

**Tech Stack:** Go 1.26、Cobra、bbolt、GitHub Actions YAML/shell、Go testing、race detector、go vet。

## Global Constraints

- 本批采用“最小局部修复 + 测试先行验收”。
- 保持现有 CLI 参数名称和成功路径不变。
- 错误输入从静默成功变为非零失败，这是有意的行为修正。
- `--no-state` 在项目模式下不得创建数据库或 BatchWriter。
- 不实现完整 crack mode 重构、全量 proxy 迁移、sink 并行化或未经 benchmark 证明的性能优化。
- 每个主题独立提交；不得夹带无关格式化或重构。
- 必须最终运行 `go test ./...`、`go test -race ./...` 和 `go vet ./...`。

## 文件地图

- `.github/workflows/release.yml`：严格校验 release 二进制、checksum 和原生 Linux smoke。
- `internal/core/credential/pool.go`：统一 inline/file credential loading 的入口复用。
- `internal/core/pipeline.go`：生产凭据池构造和加载错误返回。
- `internal/types/config.go`：端口配置解析和有效 include/exclude 端口规则。
- `internal/core/scanner.go`：在启动 goroutine 前完成端口解析并将错误返回。
- `cmd/scan.go`：`NoState` 下不创建 Store/BatchWriter，并保留 CLI 错误向上返回。
- `internal/core/alive/cmd.go`：移除共享 timeout 字段写入。
- `internal/core/scan/retry.go`：将 retry counters 改为原子统计。
- `internal/core/scan/prescreen.go`：让 task producer 对 Context 取消可退出。
- 对应 `*_test.go`：为每项行为添加最小回归测试；优先修改现有测试文件，不创建重复 fixture。

---

### Task 1: 修复 release checksum fail-open 和产物 smoke

**Files:**
- Modify: `.github/workflows/release.yml`（artifact merge、checksum 和 publish 前验证步骤）
- Test/verify: `.github/workflows/release.yml` 的 shell 逻辑；本地只做 YAML/脚本静态检查，不伪造 GitHub release

**Interfaces:**
- Consumes: release job 合并后的五个目标平台二进制和 tag-derived version。
- Produces: 非空且恰好覆盖五个预期二进制的 `SHA256SUMS`；Linux 原生二进制 `version`/`--help` smoke 结果。

- [ ] **Step 1: 读取现有 release 产物命名和 publish job**

运行：

```bash
git show HEAD:.github/workflows/release.yml
```

确认五个实际二进制名称，后续只使用这些明确名称，不用宽泛 `*.exe` 或 `*-*` glob。

- [ ] **Step 2: 修改 checksum 逻辑为 fail-closed**

使用严格 shell 模式并显式检查每个文件：

```bash
set -euo pipefail
expected=(...明确的五个二进制名称...)
for artifact in "${expected[@]}"; do
  test -s "$artifact"
done
sha256sum "${expected[@]}" > SHA256SUMS
test "$(wc -l < SHA256SUMS)" -eq "${#expected[@]}"
sha256sum -c SHA256SUMS
```

保留现有签名、证书和 SBOM 文件，不将它们加入 binary checksum 清单。

- [ ] **Step 3: 添加 Linux native version/help smoke**

在签名和发布前执行 Linux amd64 binary：

```bash
version_output="$(./fg-qimen-linux-amd64 version)"
printf '%s\n' "$version_output" | grep -F "${GITHUB_REF_NAME#v}"
./fg-qimen-linux-amd64 --help >/dev/null
```

如果现有 CLI 版本输出格式不同，按实际输出断言 tag 版本，不改变 CLI 输出格式。

- [ ] **Step 4: 静态验证 workflow**

运行：

```bash
git diff --check
```

并使用仓库已有的 YAML 校验方式；若本机没有对应工具，只报告未执行，不通过修改业务代码绕过。

- [ ] **Step 5: 提交**

```bash
git add .github/workflows/release.yml
git commit -m "ci: fail closed on release artifact checksums" -m "Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: 让 credential 文件参数进入生产路径

**Files:**
- Modify: `internal/core/pipeline.go`
- Inspect/modify as needed: `internal/core/credential/pool.go`
- Test: `internal/core/pipeline_test.go` 或现有 credential pool 测试文件
- Test: `cmd` 集成测试所在文件（若已有临时目录/命令 fixture，复用它）

**Interfaces:**
- Consumes: `types.Config.Users`, `Passes`, `UserFile`, `PassFile`。
- Produces: production scan 使用同一个 credential loader 生成的 credential slice；文件读取、限制和解析错误返回给 `RunScan`。

- [ ] **Step 1: 写 file-only 和 mixed credentials 失败测试**

测试必须验证生产构造函数，而不只测 loader：

```go
func TestLoadCredsUsesFiles(t *testing.T) {
    dir := t.TempDir()
    users := filepath.Join(dir, "users.txt")
    passes := filepath.Join(dir, "passes.txt")
    require.NoError(t, os.WriteFile(users, []byte("alice\nbob\n"), 0600))
    require.NoError(t, os.WriteFile(passes, []byte("one\ntwo\n"), 0600))

    got, err := loadCreds(types.Config{UserFile: users, PassFile: passes})
    require.NoError(t, err)
    require.Len(t, got, 4)
}
```

按项目实际 credential 类型和断言库调整代码，但测试必须覆盖 file-only、inline+file、不可读文件和上限错误。

- [ ] **Step 2: 运行测试确认当前实现失败**

```bash
go test ./internal/core ./internal/core/credential/... -run 'TestLoadCredsUsesFiles|Test.*Credential.*File' -count=1
```

预期：file-only 测试失败或返回空 credential slice。

- [ ] **Step 3: 修改生产 loader**

让 `core.loadCreds` 调用现有 `credential.LoadInto` 或其当前签名对应的统一 loader，传入四类输入：

```text
Users, Passes, UserFile, PassFile
```

保留现有限制、去重和错误类型；不可读文件和超限不能转换为空列表或只写日志。

- [ ] **Step 4: 补齐生产路径错误传播测试**

增加不可读文件测试，断言 `RunScan`/生产构造函数返回非 nil error，并断言不会启动网络 worker。增加 mixed inline/file 去重测试。

- [ ] **Step 5: 运行相关测试**

```bash
go test ./internal/core ./internal/core/credential/... ./cmd -run 'Test.*Cred|Test.*Credential' -count=1
```

- [ ] **Step 6: 提交**

```bash
git add internal/core/pipeline.go internal/core/credential/pool.go internal/core/*test.go cmd/*test.go
git commit -m "fix: load credential dictionaries in production scans" -m "Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: 实现 exclude ports 并同步返回非法端口错误

**Files:**
- Modify: `internal/types/config.go`
- Modify: `internal/core/scanner.go`
- Test: `internal/types/config_test.go`
- Test: `internal/core/scanner_test.go` 或现有 scanner tests

**Interfaces:**
- Consumes: `Config.Ports` 和 `Config.ExcludePorts`。
- Produces: 一个同步解析、去重且排除后的 `[]int`；非法输入在启动 goroutine 前返回 error。

- [ ] **Step 1: 写端口解析单元测试**

覆盖：

```text
include 22,80,443 + exclude 80 -> 22,443
重复 include/exclude -> 去重
非法 token -> error
越界端口 -> error
排除后为空 -> 明确 error 或符合现有空集合契约
```

示例：

```go
func TestResolvePortsExcludesConfiguredPorts(t *testing.T) {
    cfg := types.Config{Ports: "22,80,443", ExcludePorts: "80"}
    got, err := cfg.ResolvePorts()
    require.NoError(t, err)
    require.Equal(t, []int{22, 443}, got)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/types ./internal/core -run 'TestResolvePorts|Test.*Exclude.*Port' -count=1
```

- [ ] **Step 3: 实现单一 effective-port resolver**

在 `types.Config` 或已有 port parser 所属包中实现统一函数：

```go
func (c Config) ResolvePorts() ([]int, error)
```

解析 include/default ports，解析 exclusions，使用集合做去重和相减，按稳定升序返回。不要在 worker 内重复解析。

- [ ] **Step 4: 将解析前移到 `RunScan`**

在启动 pipeline goroutine、alive worker 或 port worker 前调用 `ResolvePorts`。错误直接返回，不再在 goroutine 中 log 后 return nil。将结果传给 iterator/scanner，确保 excluded ports 不可能进入 probe。

- [ ] **Step 5: 添加 fake probe 端到端断言**

使用现有 iterator/probe fake，记录收到的端口，断言 exclude 集合没有任何调用。

- [ ] **Step 6: 运行测试并提交**

```bash
go test ./internal/types ./internal/core ./internal/config -count=1
git add internal/types/config.go internal/core/scanner.go internal/types/*test.go internal/core/*test.go
git commit -m "fix: apply excluded ports before scanning" -m "Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: 兑现 no-state 语义

**Files:**
- Modify: `cmd/scan.go`
- Inspect/modify: `internal/workspace/workspace.go` only if Store is opened too early to avoid
- Test: `cmd` integration test file using `t.TempDir()`

**Interfaces:**
- Consumes: `types.Config.NoState` and project workspace settings。
- Produces: `Session.Store == nil` and no `fg.db` creation when NoState is true。

- [ ] **Step 1: 写临时项目 no-state 测试**

```go
func TestProjectNoStateDoesNotCreateDatabase(t *testing.T) {
    dir := t.TempDir()
    cfg := types.Config{Project: "audit", ProjectRoot: dir, NoState: true}
    sess, cleanup, err := buildSessionForTest(cfg)
    require.NoError(t, err)
    t.Cleanup(cleanup)
    require.Nil(t, sess.Store)
    _, err = os.Stat(filepath.Join(dir, "audit", "fg.db"))
    require.ErrorIs(t, err, fs.ErrNotExist)
}
```

按现有 workspace/session 工厂签名适配，不创建新的生产测试 API，优先复用已有测试 helper。

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./cmd ./internal/workspace -run 'Test.*NoState' -count=1
```

- [ ] **Step 3: 在 session/workspace 构造处阻止 Store**

当 `cfg.NoState` 为 true 时：

- 不打开 bbolt
- `sess.Store = nil`
- 不创建 BatchWriter
- 仍允许非持久化输出正常运行

不要仅在 `RunScan` 中禁用写入，因为 workspace 初始化阶段也不能产生数据库。

- [ ] **Step 4: 验证 project/no-state 与普通 ephemeral path**

```bash
go test ./cmd ./internal/workspace ./internal/core -run 'Test.*NoState|Test.*Project' -count=1
```

- [ ] **Step 5: 提交**

```bash
git add cmd/scan.go internal/workspace/workspace.go cmd/*test.go internal/workspace/*test.go
git commit -m "fix: honor no-state project scans" -m "Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: 修复 cmdProbe 和 RetryStats 竞态

**Files:**
- Modify: `internal/core/alive/cmd.go`
- Modify: `internal/core/scan/retry.go`
- Test: `internal/core/alive/cmd_test.go`
- Test: `internal/core/scan/retry_test.go`

**Interfaces:**
- Consumes: 现有 `Probe(ctx, host, timeout)` 和 RetryStats API。
- Produces: 并发调用无共享 timeout 写入；统计值使用 atomic 或等价同步且 API 行为不变。

- [ ] **Step 1: 添加并发 timeout regression test**

并发启动多个 `Probe` 调用，使用 fake command runner 或现有可注入接口，确保测试不依赖真实 ICMP/系统命令，并在 race 下运行。

- [ ] **Step 2: 修改 timeout 为局部变量**

删除或停止写入 `cmdProbe.timeout` 字段，在 `Probe` 内计算：

```go
effective := 5 * time.Second
if timeout > 0 && timeout < effective {
    effective = timeout
}
```

所有命令 context/deadline 使用 `effective`。

- [ ] **Step 3: 添加 RetryStats 并发测试**

使用固定次数并发 probe，断言 `TotalAttempts` 等统计值不丢失。测试必须通过 `-race`。

- [ ] **Step 4: 将计数改为 atomic**

优先使用 `atomic.Int64` 字段；如果公开结构不能改变，则增加内部 atomic counters，并在读取 API 时加载。保持外部统计字段语义和现有测试兼容。

- [ ] **Step 5: 运行 race 相关测试**

```bash
go test -race ./internal/core/alive ./internal/core/scan -count=1
```

- [ ] **Step 6: 提交**

```bash
git add internal/core/alive/cmd.go internal/core/alive/*test.go internal/core/scan/retry.go internal/core/scan/*test.go
git commit -m "fix: make probe timeout and retry stats race-safe" -m "Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: 修复 prescreen 的可取消退出

**Files:**
- Modify: `internal/core/scan/prescreen.go`
- Test: `internal/core/scan/prescreen_test.go`

**Interfaces:**
- Consumes: `context.Context`、prescreen task 列表和现有 semaphore。
- Produces: Context 取消后不再生产新 task，且不会卡在 semaphore acquire。

- [ ] **Step 1: 写 cancellation regression test**

构造一个已填满 semaphore 的 prescreen，取消 context，断言函数在有限时间内返回，不等待完整 probe timeout，也不新增取消后的任务。

```go
ctx, cancel := context.WithCancel(context.Background())
cancel()
done := make(chan struct{})
go func() {
    _, _ = probSegments(ctx, tasks, probe, timeout)
    close(done)
}()
select {
case <-done:
case <-time.After(500 * time.Millisecond):
    t.Fatal("prescreen did not stop after cancellation")
}
```

按现有函数签名和 fake probe 适配。

- [ ] **Step 2: 运行测试确认当前行为**

```bash
go test ./internal/core/scan -run 'Test.*Prescreen.*Cancel|Test.*ProbSegments' -count=1
```

- [ ] **Step 3: 使用 labeled break 和可取消 acquire**

将 task loop 改为等价结构：

```go
loop:
for _, task := range tasks {
    select {
    case <-ctx.Done():
        break loop
    case sem <- struct{}{}:
    }
    // 仅在成功取得 semaphore 后创建 worker
}
```

确保每个成功 acquire 都有对应 release，且 `wg.Wait()` 仍然等待已经创建的 worker。

- [ ] **Step 4: 运行 scan 包测试和 race**

```bash
go test ./internal/core/scan -count=1
go test -race ./internal/core/scan -count=1
```

- [ ] **Step 5: 提交**

```bash
git add internal/core/scan/prescreen.go internal/core/scan/prescreen_test.go
git commit -m "fix: stop prescreen promptly on cancellation" -m "Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: 集成验证与结果审查

**Files:**
- Modify only if tests expose a regression; otherwise no source changes.
- Review: all files changed by Tasks 1-6.

**Interfaces:**
- Consumes: all preceding commits and their tests。
- Produces: verified first-batch repair set and documented residual risks。

- [ ] **Step 1: 查看工作树和提交范围**

```bash
git status --short
git log --oneline -8
git diff HEAD~6..HEAD --stat
```

确认没有无关文件、生成物或临时数据库。

- [ ] **Step 2: 运行完整单元测试**

```bash
go test ./...
```

预期：所有包通过。

- [ ] **Step 3: 运行全仓 race**

```bash
go test -race ./...
```

预期：无 race、无 timeout；若 Windows 环境导致特定包无法执行，记录具体包和原因，不隐藏失败。

- [ ] **Step 4: 运行 vet 和 diff 检查**

```bash
go vet ./...
git diff --check
```

- [ ] **Step 5: 审查配置行为**

手工或集成测试确认：

```text
file-only credential -> 进入认证
exclude port -> 不进入 probe
invalid port -> 非零错误
project + no-state -> 不创建 fg.db
```

- [ ] **Step 6: 更新任务状态和输出残余风险**

最终报告必须明确：本批没有实现完整 crack mode、全量 proxy 统一、所有认证器 read deadline、sink 错误全链路传播和性能重构；这些保留到后续迭代。

- [ ] **Step 7: 提交最终验证修复（如有）**

只有验证过程中发现并修复本批范围内的回归时才提交：

```bash
git add <仅限本批相关文件>
git commit -m "test: close first-batch verification gaps" -m "Co-Authored-By: Claude <noreply@anthropic.com>"
```
