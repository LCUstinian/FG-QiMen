# FG-QiMen 全面优化实施计划

## 概述

基于fscan技术对比分析，在保持FG-QiMen"无漏洞利用"核心原则下，通过5个阶段提升性能和功能。

## 阶段划分

### ✅ 阶段1：网络层增强（1-2周）
**目标**：引入全局代理管理、网卡指定、独立超时配置

**实施步骤**：
1. 创建 `internal/network/proxy/` 包
   - `manager.go`: 全局代理管理器（单例模式）
   - `types.go`: ProxyConfig/ProxyType定义
   - `socks5.go`: SOCKS5拨号器
   - `http.go`: HTTP/HTTPS拨号器
   - `validator.go`: 4阶段连接验证
   
2. 修改 `internal/types/config.go`
   - 添加 ProxyConfig 结构体
   - 添加 Iface 网卡字段
   - 添加独立超时字段（PortTimeout/WebTimeout/GlobalTimeout）

3. 修改 `cmd/flags.go`
   - 添加 `-proxy`, `-socks5`, `-iface` 参数
   - 添加独立超时参数

4. 修改各插件适配全局代理
   - `internal/plugins/adapted/web/webtitle/probe.go`
   - `internal/plugins/adapted/database/*/`
   - 其他需要网络连接的插件

**文件清单**：
- 新增：`internal/network/proxy/*.go` (5个文件)
- 修改：`internal/types/config.go`
- 修改：`cmd/flags.go`
- 修改：20+个插件文件

**验证**：
```bash
# 测试SOCKS5代理
fg-qimen -H 192.168.1.1 -socks5 127.0.0.1:1080

# 测试HTTP代理
fg-qimen -H 192.168.1.1 -proxy http://127.0.0.1:8080

# 测试网卡指定
fg-qimen -H 192.168.1.1 -iface 192.168.2.100
```

---

### ✅ 阶段2：扫描能力增强（2-3周）
**目标**：扩展默认端口、端口组支持、网段预筛、扫描重试

**实施步骤**：
1. 创建 `internal/config/ports.go`
   - MainPorts: 133个端口常量
   - WebPorts: 284个Web端口
   - DbPorts/ServicePorts/CommonPorts定义
   - PortGroups映射（web/db/service/common/all）

2. 创建 `internal/core/scan/prescreen.go`
   - 网段预筛逻辑（网关探测.1/.254）
   - 轮换端口策略（22/80/443/3389）
   - 跳过空网段

3. 修改 `internal/core/scan/scanner.go`
   - 集成网段预筛（超256主机时启用）
   - 添加失败端口收集器
   - 端口扫描重试（仅资源耗尽错误）

4. 修改 `cmd/flags.go`
   - 支持端口组参数（`-p web`, `-p db`等）
   - 保持向后兼容（数字端口列表）

**文件清单**：
- 新增：`internal/config/ports.go`
- 新增：`internal/core/scan/prescreen.go`
- 修改：`internal/core/scan/scanner.go`
- 修改：`internal/core/scan/iterator.go`（端口喷洒）
- 修改：`cmd/flags.go`

**验证**：
```bash
# 测试端口组
fg-qimen -H 192.168.1.1 -p web
fg-qimen -H 192.168.1.1 -p db
fg-qimen -H 192.168.1.1 -p all

# 测试网段预筛（大网段）
fg-qimen -H 10.0.0.0/16 -p 22,80,443
```

---

### ✅ 阶段3：性能优化（1-2周）
**目标**：正则预编译、零分配优化、滑动窗口调度器、ICMP令牌桶

**实施步骤**：
1. 正则预编译
   - 修改 `internal/portscan/fingerprint/*.go`
   - 修改 `internal/plugins/adapted/web/webtitle/*.go`
   - 提升所有 regexp.MustCompile 为包级变量

2. 零分配优化
   - 创建 `internal/utils/strconv.go`
     - `fmtPort(host, port)` 无分配端口格式化
     - `containsFold(s, substr)` 无分配大小写匹配
   - 替换所有 `fmt.Sprintf("%s:%d")` 为 `fmtPort`

3. 滑动窗口调度器
   - 重构 `internal/core/scan/pool.go`
     - 新增 `slidingWindowSchedule()` 方法
     - 维护固定窗口大小（恒定内存O(windowSize)）
   - 保留自适应调整能力

4. ICMP令牌桶限速
   - 修改 `internal/core/alive/icmp.go`
   - 添加 `rateLimiter` 字段（默认1000 pps）
   - 防止路由器过载

**文件清单**：
- 修改：`internal/portscan/fingerprint/*.go` (所有文件)
- 修改：`internal/plugins/adapted/web/webtitle/*.go`
- 新增：`internal/utils/strconv.go`
- 重构：`internal/core/scan/pool.go`
- 修改：`internal/core/alive/icmp.go`

**验证**：
```bash
# 性能基准测试
go test -bench=. ./internal/core/scan/
go test -bench=. ./internal/portscan/fingerprint/

# 内存分析
go test -memprofile=mem.out ./internal/core/scan/
go tool pprof mem.out
```

---

### ✅ 阶段5：SDK封装（2-3周）
**目标**：pkg/fg-qimen SDK、任务控制、进度回调、流式结果处理

**实施步骤**：
1. 创建 `pkg/fg-qimen/` 包结构
   - `scanner.go`: Scanner结构体、NewScanner()
   - `config.go`: 外部Config定义
   - `controller.go`: Controller（Pause/Resume/Cancel/Stats）
   - `types.go`: Target/ScanProgress/ScanStats/Result
   - `examples/`: 使用示例

2. 重构 `internal/core/pipeline.go`
   - 添加暂停/恢复信号通道
   - 支持进度回调hook
   - 流式结果推送（避免内存积压）

3. 创建进度回调机制
   - 500ms轮询ticker
   - 包含：已扫描主机数、端口数、发现服务数、凭据命中数

4. 编写SDK文档和示例
   - `pkg/fg-qimen/README.md`
   - `pkg/fg-qimen/examples/basic/main.go`
   - `pkg/fg-qimen/examples/with-controller/main.go`

**文件清单**：
- 新增：`pkg/fg-qimen/*.go` (10+个文件)
- 修改：`internal/core/pipeline.go`
- 修改：`internal/session/session.go`（添加控制信号）
- 新增：`pkg/fg-qimen/examples/`

**验证**：
```go
// 基本使用
scanner := fgqimen.NewScanner(fgqimen.Config{
    Targets: []fgqimen.Target{{Host: "192.168.1.0/24"}},
})
results, err := scanner.Scan(context.Background())

// 带控制器
ctrl, resultCh, errCh := scanner.ScanWithController(ctx)
ctrl.Pause()
time.Sleep(5 * time.Second)
ctrl.Resume()
stats := ctrl.Stats()
```

---

### ✅ 阶段6：工程化（持续）
**目标**：单元测试覆盖率、性能基准测试、多语言支持

**实施步骤**：
1. 单元测试覆盖率提升
   - 核心模块：scan/credential/output/store
   - 目标：80%+覆盖率
   - 创建测试辅助工具（fake server等）

2. 性能基准测试
   - `internal/core/scan/benchmark_test.go`
   - `internal/portscan/fingerprint/benchmark_test.go`
   - 测试端口扫描吞吐量、指纹匹配性能

3. 多语言支持（i18n）
   - 创建 `internal/i18n/` 包
   - 使用 `go-i18n` 库
   - 支持中英文切换
   - CLI添加 `-lang` 参数

4. CI/CD优化
   - `.github/workflows/test.yml`: 自动测试
   - `.github/workflows/benchmark.yml`: 性能回归检测
   - golangci-lint配置

**文件清单**：
- 新增：`internal/*/\*_test.go` (50+个测试文件)
- 新增：`internal/i18n/*.go`
- 新增：`.github/workflows/*.yml`
- 修改：`go.mod`（添加测试依赖）

**验证**：
```bash
# 测试覆盖率
go test -cover ./internal/...

# 性能基准
go test -bench=. -benchmem ./internal/...

# 语言切换
fg-qimen -H 192.168.1.1 -lang zh
fg-qimen -H 192.168.1.1 -lang en
```

---

## 实施时间线

| 阶段 | 工期 | 优先级 | 依赖 |
|------|------|--------|------|
| 阶段1：网络层增强 | 1-2周 | P0 | 无 |
| 阶段2：扫描能力增强 | 2-3周 | P0 | 无 |
| 阶段3：性能优化 | 1-2周 | P0 | 阶段2（pool重构） |
| 阶段5：SDK封装 | 2-3周 | P1 | 阶段1+2（功能完备后封装） |
| 阶段6：工程化 | 持续 | P2 | 所有阶段（并行进行） |

**总工期**：8-12周（约2-3个月）

---

## 风险控制

1. **向后兼容性**
   - 所有新参数保持可选
   - 默认行为与v0.2保持一致
   - 端口参数支持旧格式（数字列表）

2. **增量发布**
   - 每个阶段独立测试验证
   - 可独立发布小版本（v0.3.1/v0.3.2/...）
   - 避免大爆炸式重构

3. **性能回归**
   - 建立性能基准baseline
   - 每次优化前后对比
   - 自动化性能测试

4. **功能验证**
   - 保留原有功能测试用例
   - 新增功能独立测试
   - 端到端集成测试

---

## 成功指标

### 阶段1（网络层）
- ✅ 支持SOCKS5/HTTP代理
- ✅ 代理场景性能提升60%+（连接复用）
- ✅ 支持网卡指定

### 阶段2（扫描能力）
- ✅ 默认端口从6个扩展到133个
- ✅ 大网段扫描速度提升40%+（网段预筛）
- ✅ 支持5种端口组（web/db/service/common/all）

### 阶段3（性能优化）
- ✅ 内存占用降低20%+（零分配优化）
- ✅ 正则匹配性能提升50%+（预编译）
- ✅ ICMP扫描稳定性提升（令牌桶限速）

### 阶段5（SDK封装）
- ✅ 提供完整Go SDK
- ✅ 支持Pause/Resume/Cancel
- ✅ 500ms实时进度回调

### 阶段6（工程化）
- ✅ 核心模块测试覆盖率80%+
- ✅ 支持中英文切换
- ✅ CI/CD自动化测试

---

## 技术参考

### fscan借鉴技术点（TOP 10）
1. ⭐⭐⭐⭐⭐ 全局代理管理器（network.go L27-47）
2. ⭐⭐⭐⭐⭐ pkg/fscan SDK架构
3. ⭐⭐⭐⭐ 网段预筛机制（port_scan.go L756-879）
4. ⭐⭐⭐⭐ 正则预编译（portfinger/version_parser.go）
5. ⭐⭐⭐⭐ 端口组管理（config/constants.go L20-58）
6. ⭐⭐⭐ 滑动窗口调度器（port_scan.go L293-321）
7. ⭐⭐⭐ 零分配优化（port_scan.go L324-420）
8. ⭐⭐⭐ ResultBuffer双写机制（output/buffer.go）
9. ⭐⭐⭐ 自适应超时（port_scan.go L212-232）
10. ⭐⭐⭐ DefaultPortMap（config/constants.go L91-222）

### 排除技术点
- ❌ 漏洞利用（Redis写公钥/SSH命令执行/MS17-010等）
- ❌ 本地模块（信息收集/凭据获取/权限维持）
- ❌ POC扫描（Web漏洞检测）
- ❌ 后渗透（反弹Shell/SOCKS5代理服务端）

---

## 下一步行动

1. ✅ 创建此计划文档
2. ⏳ 执行阶段1：网络层增强
3. ⏳ 执行阶段2：扫描能力增强
4. ⏳ 执行阶段3：性能优化
5. ⏳ 执行阶段5：SDK封装
6. ⏳ 执行阶段6：工程化

**当前状态**：计划模式，待批准后开始实施。
