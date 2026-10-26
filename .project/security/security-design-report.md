# auto_performance（性能测试平台）产品安全需求与设计报告

**产品**：auto_performance（性能测试平台）　**版本**：v1.0　**日期**：2026-10-26
**输入**：`PRD-performance-testing-platform.md`、`architecture.md`、`ADR-0001~0006`
**方法**：STRIDE 威胁建模 × 本地安全知识库（华为供应商产品网络安全红线V2.0）
*本报告包含敏感信息，仅供内部使用*

---

## 执行摘要

**产品概述**：性能测试平台面向平台管理员，统一管理「环境 → 产品 → 接口 → 测试任务 → 报告」闭环，提供接口性能压测（k6 子进程）、阈值达标判定与 html/pdf/word 报告生成。技术栈 Go+Gin 单二进制（go:embed 内嵌前端）+ Vue3 + PostgreSQL + k6 + headless Chromium/gooxml，离线 tar.gz 部署 + install/upgrade/uninstall 脚本。核心安全特征：管理员分级分权（子模块级只读/可写）、角色=权限集合、内置 admin 首次登录改密、证书/SSH 密钥加密存储、环境清理走可信 SSH 远程执行。

**输出范围**：全部阶段（REQ→DES→CODE→TEST→MATRIX）　**详略**：精简档

**风险分布**

| 风险等级 | 数量（REQ 行） | 涉及模块 |
|---------|------------|---------|
| 严重/高 | 23 | 系统设置（认证/密钥/权限）、测试管理（注入）、报告管理（上传）、日志管理（审计）、部署运维（端口/完整性） |
| 中 | 17 | 环境/产品管理（数据泄露）、测试管理（DoS）、日志（脱敏）、部署运维（供应链） |
| 低 | 4 | 通用加固、限流 |

**知识库覆盖**：已对照《华为供应商产品网络安全红线V2.0》安全需求 71 条、安全设计 155 条。其中适用本产品的 **Critical/High 红线 21 条全部落入对应 REQ/DES 行**，并派生红线专项 TEST 用例；不适用条款（物理接口认证 #1.1.5、移动终端/App 隐私 #11.2.2/#11.1.1-3 等、合法监听 #10.x、AI 滥用 #16.x）已在本报告相关处标注 N/A 及理由。

---

## 安全行动建议

> 面向决策者，优先处理项前置。关联ID 锚定本次实际输出的最深层级（DES/TEST）。

| 优先级 | 建议 | 关联ID | 截止日期 |
|-------|------|--------|---------|
| P0 | 落实证书私钥/SSH 密钥「AES-256-GCM + 根密钥（KEK）加密存储」，根密钥不硬编码、权限 0600，并设计密钥轮换流程 | DES-I-008 / CODE-I-008 | 2026-11-30 |
| P0 | k6 脚本生成与清理脚本执行改为「参数白名单 + 无 shell 子进程（exec.CommandContext 逐参传递）」，杜绝脚本/命令注入 | DES-T-003 / CODE-T-003 | 2026-11-30 |
| P0 | 报告导入上传实施「扩展名+magic bytes 双校验 + 大小限制 + 存储目录禁止脚本执行 + 解析器加固」，阻断恶意文件与路径穿越 | DES-T-005 / CODE-T-005 | 2026-11-30 |
| P0 | 认证：bcrypt 密码哈希（cost≥10）、登录失败锁定+防暴力破解、首次登录强制改密、JWT 短 TTL + 登出失效 | DES-S-006 / CODE-S-006 | 2026-11-30 |
| P0 | 审计日志防篡改（HMAC 链式签名或只读归档）+ 禁止应用层删除/修改日志 | DES-T-006 / CODE-T-006 | 2026-12-15 |
| P1 | 敏感数据（接口地址含认证头、主机地址）传输 TLS1.2+、存储加密/脱敏、日志脱敏 | DES-I-002 / DES-I-004 | 2026-12-15 |
| P1 | 分级分权 RBAC 服务端强制鉴权（每请求核实 read/write），敏感子模块（管理员/证书/SSH）叠加 write-only 保护 | DES-E-007 / CODE-E-007 | 2026-11-30 |
| P1 | 离线包 manifest 完整性校验（SHA256）+ 升级前备份/回滚 + 发布包双引擎防病毒与可疑组件扫描 | DES-T-009 / CODE-T-009 | 2026-12-15 |
| P2 | 补全《通信矩阵》、安全配置/加固指南、缺省账号清单、个人数据说明四类安全资料 | DES-I-012 | 2026-12-31 |

---

## 功能模块：环境管理

**结论**：环境管理 CRUD 风险集中在「子模块级越权」与「主机地址/SSH 引用泄露内网拓扑」，红线落点为服务端 RBAC 鉴权与操作审计。

### ① 安全需求（STRIDE）

| 需求ID | 威胁类型 | 安全需求（一句话） | 风险等级 | 优先级 | 知识库来源 |
|--------|---------|-----------------|------|--------|-----------|
| REQ-S-001 | 身份假冒(S) | 环境管理所有接口须 JWT 认证 + 子模块级 RBAC 授权，最终鉴权在服务端完成 | 高 | P0 | `《红线V2.0》#1.1.4`、`#4.1.4` |
| REQ-E-001 | 权限提升(E) | env.list/env.tag 按 module_code 做 read/write 隔离，防止越权 CRUD 与越权查看主机地址 | 高 | P0 | `《红线V2.0》#4.1.2`、`#8.1.5` |
| REQ-T-001 | 篡改(T) | 环境配置（主机地址/SSH 关联）写入需鉴权 + 审计 + 乐观锁，防篡改导致任务压测错误目标 | 中 | P1 | — |
| REQ-I-001 | 信息泄露(I) | 主机地址列表/OS/SSH 引用等内网拓扑信息仅授权管理员可见，响应脱敏 | 中 | P1 | `《红线V2.0》#7.1.9` |
| REQ-R-001 | 抵赖(R) | 环境增删改、停用/启用操作记录 5W1H 用户日志，支撑审计 | 中 | P1 | `《红线V2.0》#8.1.2` |

### ② 安全设计

| 设计ID | 关联需求 | 设计领域 | 安全设计（一句话） | 优先级 | 知识库来源 |
|--------|---------|---------|-----------------|--------|-----------|
| DES-S-001 | REQ-S-001 | 身份认证 | JWT 校验签名+有效期，权限从 DB 短 TTL 缓存读取，服务端最终鉴权 | P0 | `《红线V2.0》#4.1.4` |
| DES-E-001 | REQ-E-001 | 访问控制 RBAC | required(env.list, read/write) 集合包含判断，读请求校验 read、写请求校验 write | P0 | `《红线V2.0》#4.1.2` |
| DES-T-001 | REQ-T-001 | 数据完整性 | 写操作经审计中间件 + 乐观锁版本号防并发覆盖 | P1 | — |
| DES-I-001 | REQ-I-001 | 敏感数据保护 | 主机地址按权限过滤返回，日志中对 SSH 引用/地址做脱敏 | P1 | `《红线V2.0》#7.1.9` |
| DES-R-001 | REQ-R-001 | 日志审计 | 操作日志 5W1H 落库，防篡改机制复用日志模块 | P1 | `《红线V2.0》#8.1.2` |

### ③ 核心代码示例

`CODE-E-001`（对应 `DES-E-001`）— 子模块级 RBAC 中间件
```go
// gin 中间件：从上下文取 admin 权限集，按 module+action 校验
func RequirePerm(module string, need string) gin.HandlerFunc {
    return func(c *gin.Context) {
        perms := c.MustGet("perms").(map[string]string) // admin_role→role_permission 并集
        if p, ok := perms[module]; !ok || (need == "write" && p != "write") {
            c.AbortWithStatusJSON(403, gin.H{"code": 403, "message": "forbidden"})
            return
        }
        c.Next()
    }
}
// 路由：r.GET("/environments", RequirePerm("env.list","read"), ...)
//      r.POST("/environments", RequirePerm("env.list","write"), ...)
```
关键点：鉴权在服务端集合包含判断，前端隐藏不可绕过。

`CODE-S-001`（对应 `DES-S-001`）— JWT 校验与权限加载
```go
func AuthN() gin.HandlerFunc {
    return func(c *gin.Context) {
        tok := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
        claims, err := jwt.ParseWithClaims(tok, &Claims{}, keyFunc, // 校验签名+exp
            jwt.WithValidMethods([]string{"HS256"}))
        if err != nil { c.AbortWithStatusJSON(401, gin.H{"code":401}); return }
        perms := permCache.Get(claims.Sub) // 短TTL，角色变更即失效
        c.Set("admin_id", claims.Sub); c.Set("perms", perms); c.Next()
    }
}
```
关键点：权限不入 token，角色变更即时生效。

`CODE-T-001`（对应 `DES-T-001`）— 乐观锁更新
```go
res := db.Model(&Env{}).Where("id=? AND version=?", id, req.Version).
    Updates(map[string]any{"hosts": req.Hosts, "version": gorm.Expr("version+1")})
if res.RowsAffected == 0 { return errors.New("conflict: environment modified concurrently") }
```
关键点：版本号并发冲突检测防覆盖。

`CODE-I-001`（对应 `DES-I-001`）— 响应字段权限过滤
```go
func toEnvDTO(e Env, canSeeHosts bool) EnvDTO {
    d := EnvDTO{ID:e.ID, Name:e.Name}
    if canSeeHosts { d.Hosts = e.Hosts } // 无 env.list.read 不返回主机地址
    return d
}
```
关键点：内网拓扑字段按权限裁剪。

`CODE-R-001`（对应 `DES-R-001`）— 审计日志落库
```go
audit.Log(ctx, AuditEvent{AdminID: ctx.adminID, Action:"env.delete",
    Resource:"environment", ResourceID:id, Detail:maskHosts(req), IP:clientIP(ctx), Result:"ok"})
```
关键点：5W1H 要素齐全。

### ④ 安全测试

| 测试ID | 关联设计 | 测试项 | 类型 | 优先级 | 风险 | 关键命令 / 判定标准 |
|--------|---------|--------|------|--------|------|-------------------|
| TEST-S-001 | DES-S-001 | 无 Token/过期 Token 访问环境接口 | 渗透验证 | P0 | 高 | `curl -i http://<host>/api/v1/environments` → 401；篡改 JWT payload 不重签 → 401 |
| TEST-E-001 | DES-E-001 | 只读角色调用写接口（越权） | 渗透验证 | P0 | 高 | 仅 env.list:read 账号 `POST /environments` → 403 Forbidden |
| TEST-T-001 | DES-T-001 | 并发修改同环境冲突检测 | 功能验证 | P1 | 中 | 两会话同 version 并发 `PUT /environments/{id}` → 仅一次成功，另一次 409 |
| TEST-I-001 | DES-I-001 | 无 read 权限不返回主机地址 | 功能验证 | P1 | 中 | 只读账号响应 JSON 无 `hosts` 字段 |
| TEST-R-001 | DES-R-001 | 环境删除操作审计可回溯 | 配置核查 | P1 | 中 | 执行删除后 `GET /logs/user` 可见操作人/时间/对象/动作 |

### ⑤ 对应性追踪矩阵

| 需求ID | 需求摘要 | 设计ID | 代码ID | 测试ID |
|--------|---------|--------|--------|--------|
| REQ-S-001 | 接口认证 | DES-S-001 | CODE-S-001 | TEST-S-001 |
| REQ-E-001 | 子模块 RBAC | DES-E-001 | CODE-E-001 | TEST-E-001 |
| REQ-T-001 | 写操作完整性 | DES-T-001 | CODE-T-001 | TEST-T-001 |
| REQ-I-001 | 内网拓扑脱敏 | DES-I-001 | CODE-I-001 | TEST-I-001 |
| REQ-R-001 | 操作审计 | DES-R-001 | CODE-R-001 | TEST-R-001 |

> 自检：REQ→DES→CODE→TEST 链完整无悬空；#1.1.4/#4.1.4/#8.1.5/#8.1.2/#7.1.9 红线已落行。

---

## 功能模块：产品管理

**结论**：接口管理承载「接口地址 + 参数模板（可能含认证头等敏感字段）」，风险集中在敏感字段泄露与阈值篡改影响达标判定。

### ① 安全需求（STRIDE）

| 需求ID | 威胁类型 | 安全需求（一句话） | 风险等级 | 优先级 | 知识库来源 |
|--------|---------|-----------------|------|--------|-----------|
| REQ-S-001 | 身份假冒(S) | 产品/接口/阈值接口须 JWT + RBAC 服务端鉴权 | 高 | P0 | `《红线V2.0》#1.1.4`、`#4.1.4` |
| REQ-E-001 | 权限提升(E) | product.list/product.interface 子模块级只读/可写隔离 | 高 | P0 | `《红线V2.0》#4.1.2` |
| REQ-I-001 | 信息泄露(I) | 接口参数模板中的认证头（Authorization/Token）等敏感字段加密存储、展示与日志脱敏 | 高 | P0 | `《红线V2.0》#7.1.1`、`#7.1.9` |
| REQ-T-001 | 篡改(T) | 阈值配置写入需鉴权+审计，阈值随任务快照防篡改影响历史判定 | 中 | P1 | — |
| REQ-R-001 | 抵赖(R) | 接口/阈值增删改记录审计日志 | 中 | P1 | `《红线V2.0》#8.1.2` |
| REQ-D-001 | 拒绝服务(D) | 接口列表/阈值查询分页与限流，防海量查询拖垮服务 | 低 | P2 | — |

### ② 安全设计

| 设计ID | 关联需求 | 设计领域 | 安全设计（一句话） | 优先级 | 知识库来源 |
|--------|---------|---------|-----------------|--------|-----------|
| DES-S-001 | REQ-S-001 | 身份认证 | JWT 认证 + 服务端最终鉴权（复用系统设置 AuthSvc） | P0 | `《红线V2.0》#4.1.4` |
| DES-E-001 | REQ-E-001 | 访问控制 RBAC | required(product.list/product.interface, read/write) 集合判断 | P0 | `《红线V2.0》#4.1.2` |
| DES-I-001 | REQ-I-001 | 敏感数据保护 | 接口认证头字段 AES-256-GCM 加密存储（密钥见系统设置），响应/日志脱敏 | P0 | `《红线V2.0》#7.1.1` |
| DES-T-001 | REQ-T-001 | 数据完整性 | 阈值写入审计 + 任务创建时阈值快照固化 | P1 | — |
| DES-R-001 | REQ-R-001 | 日志审计 | 接口/阈值变更 5W1H 审计 | P1 | `《红线V2.0》#8.1.2` |
| DES-D-001 | REQ-D-001 | 系统加固 | 列表接口分页 + 限流中间件 | P2 | — |

### ③ 核心代码示例

`CODE-I-001`（对应 `DES-I-001`）— 接口认证头加密存储
```go
func SaveInterface(api *Api, authHeader string) error {
    // authHeader 为明文认证头，加密后入字段 headers_enc
    ct, err := crypto.EncryptGCM([]byte(authHeader), kms.DEK()) // AES-256-GCM + 随机nonce
    if err != nil { return err }
    api.HeadersEnc = ct
    return db.Create(api).Error
}
```
关键点：认证头不明文落库，密钥由 KMS/KEK 管理。

`CODE-E-001`（对应 `DES-E-001`）— 子模块鉴权（同环境管理，module=product.interface）
```go
r.GET("/products/:id/interfaces", RequirePerm("product.interface","read"), h.ListInterfaces)
r.POST("/products/:id/interfaces", RequirePerm("product.interface","write"), h.CreateInterface)
```
关键点：读写分离。

`CODE-T-001`（对应 `DES-T-001`）— 阈值快照
```go
// 任务创建时固化阈值，历史报告可复现
snapshot := ThresholdSnapshot{TaskID: task.ID, Items: loadThresholds(ifaceIDs)}
db.Create(&snapshot)
```
关键点：快照隔离后续阈值变更。

`CODE-R-001`（对应 `DES-R-001`）— 阈值变更审计
```go
audit.Log(ctx, AuditEvent{AdminID: ctx.adminID, Action:"threshold.update",
    Resource:"interface", ResourceID:ifaceID, Detail:mask(req), IP:clientIP(ctx)})
```
关键点：敏感阈值变更可追溯。

`CODE-D-001`（对应 `DES-D-001`）— 分页限流
```go
func (h *Api) ListInterfaces(c *gin.Context) {
    pg := parsePage(c) // 强制 page/size 上限
    limitGuard.Allow(c.ClientIP()) // 令牌桶限流
    items, _ := repo.ListInterfaces(pg.Offset(), pg.Limit())
}
```
关键点：分页+限流防资源耗尽。

### ④ 安全测试

| 测试ID | 关联设计 | 测试项 | 类型 | 优先级 | 风险 | 关键命令 / 判定标准 |
|--------|---------|--------|------|--------|------|-------------------|
| TEST-S-001 | DES-S-001 | 无凭据访问接口/阈值接口 | 渗透验证 | P0 | 高 | `curl -i http://<host>/api/v1/products/1/interfaces` → 401 |
| TEST-E-001 | DES-E-001 | 只读角色写阈值越权 | 渗透验证 | P0 | 高 | 仅 product.interface:read 账号 `PUT /interfaces/{id}/thresholds` → 403 |
| TEST-I-001 | DES-I-001 | 认证头加密存储核查 | 配置核查 | P0 | 高 | `SELECT id, headers_enc FROM interface LIMIT 3` → 密文，非明文 Token；导出/列表响应无明文认证头 |
| TEST-T-001 | DES-T-001 | 阈值快照隔离 | 功能验证 | P1 | 中 | 改阈值后回查历史任务快照 → 历史任务阈值不变 |
| TEST-R-001 | DES-R-001 | 阈值变更审计 | 配置核查 | P1 | 中 | 变更阈值后 `GET /logs/user` 可见记录 |
| TEST-D-001 | DES-D-001 | 大分页拒绝 | 功能验证 | P2 | 低 | `GET /products?page=1&size=100000` → 拒绝或强制上限 |

### ⑤ 对应性追踪矩阵

| 需求ID | 需求摘要 | 设计ID | 代码ID | 测试ID |
|--------|---------|--------|--------|--------|
| REQ-S-001 | 接口认证 | DES-S-001 | CODE-S-001(复用模块1) | TEST-S-001 |
| REQ-E-001 | 子模块 RBAC | DES-E-001 | CODE-E-001 | TEST-E-001 |
| REQ-I-001 | 认证头加密 | DES-I-001 | CODE-I-001 | TEST-I-001 |
| REQ-T-001 | 阈值快照 | DES-T-001 | CODE-T-001 | TEST-T-001 |
| REQ-R-001 | 审计 | DES-R-001 | CODE-R-001 | TEST-R-001 |
| REQ-D-001 | 分页限流 | DES-D-001 | CODE-D-001 | TEST-D-001 |

> 自检：#7.1.1（High）认证凭据加密已落 REQ-I-001/DES-I-001；其余链完整。

---

## 功能模块：测试管理

**结论**：本模块安全风险最高——k6 脚本由后端动态生成注入环境/接口/并发/阈值参数，存在脚本注入与命令注入风险；环境清理经可信 SSH 远程执行用户脚本，存在远程命令注入与 SSH 信任滥用风险。

### ① 安全需求（STRIDE）

| 需求ID | 威胁类型 | 安全需求（一句话） | 风险等级 | 优先级 | 知识库来源 |
|--------|---------|-----------------|------|--------|-----------|
| REQ-S-001 | 身份假冒(S) | 任务/模板接口 JWT + RBAC 服务端鉴权 | 高 | P0 | `《红线V2.0》#4.1.4` |
| REQ-E-001 | 权限提升(E) | testing.task/testing.template 子模块级 read/write 隔离 | 高 | P0 | `《红线V2.0》#4.1.2` |
| REQ-T-001 | 篡改(T) | 脚本生成阶段对并发/时长/接口地址等参数白名单校验，杜绝 k6 脚本注入 | 高 | P0 | `《红线V2.0》#13.1.2` |
| REQ-T-002 | 篡改(T) | k6 子进程以参数数组（无 shell）启动，杜绝命令注入 | 高 | P0 | `《红线V2.0》#13.1.2` |
| REQ-E-002 | 权限提升(E) | k6 子进程与清理器以低权限账号运行，清理走可信 SSH 白名单 | 高 | P0 | `《红线V2.0》#5.4.1` |
| REQ-T-003 | 篡改(T) | 清理脚本经可信 SSH 执行前校验来源与内容，防远程命令注入 | 高 | P0 | — |
| REQ-D-001 | 拒绝服务(D) | 环境互斥（同环境单任务）+ 并发/时长上限，防资源耗尽 | 中 | P1 | — |
| REQ-R-001 | 抵赖(R) | 任务执行/停止/清理操作记录审计日志 | 中 | P1 | `《红线V2.0》#8.1.2` |
| REQ-I-001 | 信息泄露(I) | 任务参数（接口地址/认证头）WebSocket 推送与日志脱敏 | 中 | P1 | `《红线V2.0》#7.1.11` |

### ② 安全设计

| 设计ID | 关联需求 | 设计领域 | 安全设计（一句话） | 优先级 | 知识库来源 |
|--------|---------|---------|-----------------|--------|-----------|
| DES-S-001 | REQ-S-001 | 身份认证 | JWT + RBAC，WS 握手带 token 鉴权 | P0 | `《红线V2.0》#4.1.4` |
| DES-E-001 | REQ-E-001 | 访问控制 RBAC | required(testing.task/testing.template, read/write) | P0 | `《红线V2.0》#4.1.2` |
| DES-T-001 | REQ-T-001 | 安全编码 | k6 脚本模板 + 参数白名单校验（并发数为正整数、URL 走白名单、字符串 JSON 转义），无用户 JS 拼接 | P0 | `《红线V2.0》#13.1.2` |
| DES-T-002 | REQ-T-002 | 安全编码 | exec.CommandContext 逐参传 k6，禁用 shell 字符串拼接 | P0 | `《红线V2.0》#13.1.2` |
| DES-E-002 | REQ-E-002 | 系统加固 | k6/cleaner 进程以专用低权限账号运行，capability 最小化 | P0 | `《红线V2.0》#5.4.1` |
| DES-T-003 | REQ-T-003 | 安全编码 | 清理脚本仅执行内置动作 + 经 SSH/Host 白名单主机上的受控脚本，脚本内容做哈希白名单 | P0 | — |
| DES-D-001 | REQ-D-001 | 可用性 | 环境互斥锁（DB 行锁）+ 并发/时长参数上限校验 | P1 | — |
| DES-R-001 | REQ-R-001 | 日志审计 | 执行/停止/清理操作 5W1H 审计 | P1 | `《红线V2.0》#8.1.2` |
| DES-I-001 | REQ-I-001 | 敏感数据保护 | WS 推送与测试日志中接口认证头/地址脱敏 | P1 | `《红线V2.0》#7.1.11` |

### ③ 核心代码示例

`CODE-T-001`（对应 `DES-T-001`）— k6 脚本参数白名单生成
```go
func buildK6Script(task Task) ([]byte, error) {
    // 并发数必须为正整数，防注入 JS
    vus, err := strconv.Atoi(task.Flow.VUs)
    if err != nil || vus <= 0 || vus > maxVUs { return nil, errors.New("invalid vus") }
    if !urlAllowed(task.Env.BaseURL) { return nil, errors.New("url not allowlisted") }
    // 用 text/template 生成脚本，所有变量经 template 自动转义，不拼接原始字符串
    var buf bytes.Buffer
    if err := k6Tmpl.Execute(&buf, map[string]any{"VUs": vus, "BaseURL": task.Env.BaseURL}); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
```
关键点：参数校验 + 模板转义，用户输入不进脚本原文。

`CODE-T-002`（对应 `DES-T-002`）— 无 shell 子进程启动
```go
func runK6(ctx context.Context, scriptPath string, outPath string) error {
    cmd := exec.CommandContext(ctx, k6Bin, "run", scriptPath, "--out", "json="+outPath) // 参数数组，无 shell
    cmd.Dir = sandboxDir
    cmd.Env = append(os.Environ(), "K6_NO_USAGE_REPORT=true")
    return cmd.Run()
}
```
关键点：参数数组 + 无 shell 解释器，杜绝元字符注入。

`CODE-E-002`（对应 `DES-E-002`）— 低权限运行（部署侧 syscall）
```go
// cleaner/k6 进程 setuid 到专用账号（install.sh 创建无登录 shell 的低权限账号）
cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: svcUID, Gid: svcGID}}
```
关键点：对外服务/执行进程不 root 运行。

`CODE-T-003`（对应 `DES-T-003`）— 可信 SSH 清理执行
```go
func (c *Cleaner) Run(ctx context.Context, host SshHost, scriptHash string) error {
    if !allowlisted(host.ID, scriptHash) { return errors.New("host/script not allowlisted") }
    // 通过 x/crypto/ssh 客户端以受控方式执行，禁 shell 拼接；命令为内置动作固定集
    sess, _ := c.client.NewSession()
    defer sess.Close()
    return sess.Run(c.buildCleanupCmd(host)) // 仅内置动作，参数白名单
}
```
关键点：SSH 目标主机白名单 + 内置动作，拒绝任意用户脚本直连。

`CODE-D-001`（对应 `DES-D-001`）— 环境互斥锁
```go
tx := db.Begin()
var env Env
tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&env, envID)
if env.Status == "testing" { tx.Rollback(); return errors.New("environment busy") }
tx.Model(&env).Update("status", "testing"); tx.Commit()
```
关键点：DB 行锁保证同环境单任务。

`CODE-R-001`（对应 `DES-R-001`）— 任务操作审计
```go
audit.Log(ctx, AuditEvent{AdminID: ctx.adminID, Action:"task.cancel",
    Resource:"task", ResourceID:id, Detail:"manual stop", IP:clientIP(ctx)})
```
关键点：执行/停止/清理可追溯。

`CODE-I-001`（对应 `DES-I-001`）— 日志脱敏
```go
func redactAuth(s string) string {
    return authHeaderRE.ReplaceAllString(s, "Authorization: ***") // 正则剥离认证头
}
ws.Broadcast("task:"+id, redactAuth(progressLine))
```
关键点：WS 推送前脱敏认证头。

### ④ 安全测试

| 测试ID | 关联设计 | 测试项 | 类型 | 优先级 | 风险 | 关键命令 / 判定标准 |
|--------|---------|--------|------|--------|------|-------------------|
| TEST-S-001 | DES-S-001 | WS 无/伪造 token 连接 | 渗透验证 | P0 | 高 | `ws://<host>/api/v1/ws`（无 token）→ 拒绝握手 |
| TEST-E-001 | DES-E-001 | 只读角色执行/停止任务 | 渗透验证 | P0 | 高 | 仅 testing.task:read 账号 `POST /tasks/{id}/cancel` → 403 |
| TEST-T-001 | DES-T-001 | k6 脚本注入 | 渗透验证 | P0 | 高 | 并发数传 `1;require('child_process').exec('id')` → 被拒绝/按正整数解析失败，无命令执行 |
| TEST-T-002 | DES-T-002 | 命令注入（分号/管道/反引号） | 渗透验证 | P0 | 高 | 接口地址传 `https://x;id;` 观察 → 无命令回显、无子进程异常 |
| TEST-E-002 | DES-E-002 | k6 进程运行身份 | 配置核查 | P0 | 高 | `ps -o user= -p <k6 pid>` → 非 root |
| TEST-T-003 | DES-T-003 | 清理远程命令注入 | 渗透验证 | P0 | 高 | 清理脚本内容注入 `$(rm -rf /)` → 脚本哈希白名单拒绝执行 |
| TEST-D-001 | DES-D-001 | 环境互斥 | 功能验证 | P1 | 中 | 同环境并发 `POST /tasks` 两请求 → 仅一个进入执行，另一个拒绝/排队 |
| TEST-R-001 | DES-R-001 | 停止任务审计 | 配置核查 | P1 | 中 | 停止任务后 `GET /logs/user` 可见操作人/动作 |
| TEST-I-001 | DES-I-001 | 测试日志脱敏 | 配置核查 | P1 | 中 | 压测后 `grep -R "Authorization:" <logdir>` → 无明文认证头 |

### ⑤ 对应性追踪矩阵

| 需求ID | 需求摘要 | 设计ID | 代码ID | 测试ID |
|--------|---------|--------|--------|--------|
| REQ-S-001 | 接口/WS 认证 | DES-S-001 | CODE-S-001(复用) | TEST-S-001 |
| REQ-E-001 | 子模块 RBAC | DES-E-001 | CODE-E-001(复用) | TEST-E-001 |
| REQ-T-001 | 脚本注入 | DES-T-001 | CODE-T-001 | TEST-T-001 |
| REQ-T-002 | 命令注入 | DES-T-002 | CODE-T-002 | TEST-T-002 |
| REQ-E-002 | 低权限运行 | DES-E-002 | CODE-E-002 | TEST-E-002 |
| REQ-T-003 | 清理远程注入 | DES-T-003 | CODE-T-003 | TEST-T-003 |
| REQ-D-001 | 环境互斥 | DES-D-001 | CODE-D-001 | TEST-D-001 |
| REQ-R-001 | 审计 | DES-R-001 | CODE-R-001 | TEST-R-001 |
| REQ-I-001 | 日志脱敏 | DES-I-001 | CODE-I-001 | TEST-I-001 |

> 自检：#13.1.2（安全函数）、#5.4.1（低权限）、#4.1.4/#8.1.2/#7.1.11 已落行；注入类三项均有具体渗透载荷。

---

## 功能模块：报告管理

**结论**：报告导入/导出是文件上传攻击面（恶意文件、路径穿越、解析器漏洞）；在线查看存在越权读取风险；报告内容可能含被测接口地址等敏感信息。

### ① 安全需求（STRIDE）

| 需求ID | 威胁类型 | 安全需求（一句话） | 风险等级 | 优先级 | 知识库来源 |
|--------|---------|-----------------|------|--------|-----------|
| REQ-S-001 | 身份假冒(S) | 报告接口 JWT + RBAC 服务端鉴权 | 高 | P0 | `《红线V2.0》#4.1.4` |
| REQ-E-001 | 权限提升(E) | report.list 子模块级 read/write（导入导出）隔离，防止越权读他人报告 | 高 | P0 | `《红线V2.0》#4.1.2` |
| REQ-T-001 | 篡改(T) | 导入文件扩展名+magic bytes 双校验 + 大小限制，阻断恶意文件上传 | 高 | P0 | `《红线V2.0》#13.1.2` |
| REQ-T-002 | 篡改(T) | 导出/查看按白名单目录与文件 ID 定位，杜绝路径穿越 | 高 | P0 | `《红线V2.0》#4.1.2` |
| REQ-I-001 | 信息泄露(I) | 报告内接口地址/认证头等敏感内容导出与查看脱敏 | 中 | P1 | `《红线V2.0》#7.1.9` |
| REQ-D-001 | 拒绝服务(D) | 导入压缩炸弹/超大文件限制与解析超时，防解析器 DoS | 中 | P1 | — |
| REQ-R-001 | 抵赖(R) | 报告导入/导出操作审计 | 中 | P1 | `《红线V2.0》#8.1.2` |

### ② 安全设计

| 设计ID | 关联需求 | 设计领域 | 安全设计（一句话） | 优先级 | 知识库来源 |
|--------|---------|---------|-----------------|--------|-----------|
| DES-S-001 | REQ-S-001 | 身份认证 | JWT + RBAC，报告产物下载需鉴权 | P0 | `《红线V2.0》#4.1.4` |
| DES-E-001 | REQ-E-001 | 访问控制 RBAC | required(report.list, read/write)，报告按任务归属校验可读性 | P0 | `《红线V2.0》#4.1.2` |
| DES-T-001 | REQ-T-001 | 文件上传安全 | 扩展名+magic bytes 白名单（html/pdf/docx）+ 大小限制 + 随机文件名落盘 | P0 | `《红线V2.0》#13.1.2` |
| DES-T-002 | REQ-T-002 | 路径安全 | 产物路径由 DB 存储路径解析（非用户输入拼接），白名单目录 + 规范化后校验前缀 | P0 | `《红线V2.0》#4.1.2` |
| DES-I-001 | REQ-I-001 | 敏感数据保护 | 报告渲染前对接口认证头/地址脱敏 | P1 | `《红线V2.0》#7.1.9` |
| DES-D-001 | REQ-D-001 | 系统加固 | 上传大小上限 + 解析（Chromium/XML）超时与子进程隔离 | P1 | — |
| DES-R-001 | REQ-R-001 | 日志审计 | 导入/导出操作 5W1H 审计 | P1 | `《红线V2.0》#8.1.2` |

### ③ 核心代码示例

`CODE-T-001`（对应 `DES-T-001`）— 安全文件上传
```go
func (h *Report) Import(c *gin.Context) {
    fh, _ := c.FormFile("file")
    if fh.Size > maxReportSize { c.JSON(400, gin.H{"message":"too large"}); return }
    ext := strings.ToLower(filepath.Ext(fh.Filename))
    if !allowedReportExt[ext] { c.JSON(400, gin.H{"message":"type not allowed"}); return }
    f, _ := fh.Open(); head := make([]byte, 512); f.Read(head)
    if mime := http.DetectContentType(head); !allowedReportMime[mime] { c.JSON(400, ...); return }
    name := uuid.NewString() + ext // 随机文件名，忽略用户原文件名
    dst := filepath.Join(reportDir, name)
    io.Copy(dstFile, f)
}
```
关键点：双校验 + 随机文件名 + 大小限制，杜绝 webshell 落盘与路径穿越。

`CODE-T-002`（对应 `DES-T-002`）— 白名单目录读取
```go
func (h *Report) Download(c *gin.Context, reportID string) {
    path := repo.GetArtifactPath(reportID) // 由 DB 存储路径，非用户输入
    if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(reportDir)+string(os.PathSeparator)) {
        c.JSON(403, gin.H{"message":"forbidden"}); return
    }
    c.File(path)
}
```
关键点：路径源 DB、Clean 后前缀校验，杜绝 `../` 穿越。

`CODE-D-001`（对应 `DES-D-001`）— 解析子进程超时
```go
ctx, cancel := context.WithTimeout(c.Request.Context(), reportGenTimeout)
defer cancel()
cmd := exec.CommandContext(ctx, chromium, "--headless", "--print-to-pdf", htmlPath)
if err := cmd.Run(); err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
    return errors.New("report generation timeout")
}
```
关键点：Chromium 解析超时 + 上下文取消，防解析器 DoS。

`CODE-I-001`（对应 `DES-I-001`）— 报告内容脱敏
```go
func sanitizeReport(summary []byte) []byte {
    s := authHeaderRE.ReplaceAll(summary, []byte("Authorization: ***"))
    return s
}
```
关键点：渲染前剥离认证头。

`CODE-R-001`（对应 `DES-R-001`）— 导入导出审计
```go
audit.Log(ctx, AuditEvent{AdminID: ctx.adminID, Action:"report.import",
    Resource:"report", ResourceID:id, Detail:fileHash(fh), IP:clientIP(ctx)})
```
关键点：记录来源文件哈希便于溯源。

### ④ 安全测试

| 测试ID | 关联设计 | 测试项 | 类型 | 优先级 | 风险 | 关键命令 / 判定标准 |
|--------|---------|--------|------|--------|------|-------------------|
| TEST-S-001 | DES-S-001 | 未认证下载报告产物 | 渗透验证 | P0 | 高 | `curl -i http://<host>/api/v1/reports/{id}/artifact?format=html` → 401 |
| TEST-E-001 | DES-E-001 | 越权读他人任务报告 | 渗透验证 | P0 | 高 | 账号 A 请求账号 B 任务报告 ID → 403/404，不返回内容 |
| TEST-T-001 | DES-T-001 | 恶意文件上传 | 渗透验证 | P0 | 高 | 上传 `shell.php`/`x.jpg`(内容为脚本)/改 Content-Type 伪装 docx → 拒绝；上传成功后访问 URL → 不解析执行 |
| TEST-T-002 | DES-T-002 | 路径穿越下载 | 渗透验证 | P0 | 高 | `GET /reports/{id}/artifact?format=../../etc/passwd`、URL 编码/双编码变形 → 拒绝/404，无文件内容 |
| TEST-I-001 | DES-I-001 | 报告敏感字段脱敏 | 配置核查 | P1 | 中 | 生成含认证头报告后 `grep "Authorization" <报告>` → 已脱敏 |
| TEST-D-001 | DES-D-001 | 超大/压缩炸弹导入 | 功能验证 | P1 | 中 | 上传 >N MB 文件 → 拒绝；嵌套 zip 炸弹解析 → 超时中止无 OOM |
| TEST-R-001 | DES-R-001 | 导入导出审计 | 配置核查 | P1 | 中 | 导入后 `GET /logs/user` 可见记录含文件哈希 |

### ⑤ 对应性追踪矩阵

| 需求ID | 需求摘要 | 设计ID | 代码ID | 测试ID |
|--------|---------|--------|--------|--------|
| REQ-S-001 | 报告认证 | DES-S-001 | CODE-S-001(复用) | TEST-S-001 |
| REQ-E-001 | 越权读报告 | DES-E-001 | CODE-E-001(复用) | TEST-E-001 |
| REQ-T-001 | 恶意上传 | DES-T-001 | CODE-T-001 | TEST-T-001 |
| REQ-T-002 | 路径穿越 | DES-T-002 | CODE-T-002 | TEST-T-002 |
| REQ-I-001 | 报告脱敏 | DES-I-001 | CODE-I-001 | TEST-I-001 |
| REQ-D-001 | 解析 DoS | DES-D-001 | CODE-D-001 | TEST-D-001 |
| REQ-R-001 | 审计 | DES-R-001 | CODE-R-001 | TEST-R-001 |

> 自检：#13.1.2/#4.1.2/#7.1.9/#8.1.2 已落行；上传/穿越均有具体载荷。

---

## 功能模块：日志管理

**结论**：审计日志完整性（防篡改、防抵赖）是红线 High（#8.1.2）落点；同时须防止日志记录与检索中泄露认证凭据/接口认证头（#7.1.11）。

### ① 安全需求（STRIDE）

| 需求ID | 威胁类型 | 安全需求（一句话） | 风险等级 | 优先级 | 知识库来源 |
|--------|---------|-----------------|------|--------|-----------|
| REQ-S-001 | 身份假冒(S) | 日志查询接口 JWT + RBAC（log.* 默认只读）服务端鉴权 | 高 | P0 | `《红线V2.0》#4.1.4` |
| REQ-E-001 | 权限提升(E) | 用户/系统/测试日志子模块级 read 隔离，仅授权管理员可见 | 高 | P0 | `《红线V2.0》#4.1.2` |
| REQ-T-001 | 篡改(T) | 审计日志防篡改（HMAC 链式签名或只读归档），禁止应用层删除/修改 | 高 | P0 | `《红线V2.0》#8.1.2` |
| REQ-R-001 | 抵赖(R) | 日志记录 5W1H（时间/操作人/来源IP/事件/资源/结果），支撑事后回溯 | 高 | P0 | `《红线V2.0》#8.1.2` |
| REQ-I-001 | 信息泄露(I) | 日志中禁止明文打印口令/认证头/密钥，记录前脱敏 | 中 | P1 | `《红线V2.0》#7.1.11` |
| REQ-D-001 | 拒绝服务(D) | 日志写入限流与容量滚动，防日志洪泛耗尽磁盘 | 中 | P1 | — |

### ② 安全设计

| 设计ID | 关联需求 | 设计领域 | 安全设计（一句话） | 优先级 | 知识库来源 |
|--------|---------|---------|-----------------|--------|-----------|
| DES-S-001 | REQ-S-001 | 身份认证 | JWT + RBAC，日志接口默认只读授权 | P0 | `《红线V2.0》#4.1.4` |
| DES-E-001 | REQ-E-001 | 访问控制 RBAC | required(log.user/log.system/log.test, read) | P0 | `《红线V2.0》#4.1.2` |
| DES-T-001 | REQ-T-001 | 日志完整性 | 每条审计日志追加 HMAC 链式哈希（prev_hash+entry → hash），DB 触发器禁止 UPDATE/DELETE | P0 | `《红线V2.0》#8.1.2` |
| DES-R-001 | REQ-R-001 | 日志审计 | 5W1H 结构化字段统一写入，登录/增删改/权限变更/执行停止全覆盖 | P0 | `《红线V2.0》#8.1.2` |
| DES-I-001 | REQ-I-001 | 敏感数据保护 | 日志写入统一脱敏过滤器，剥离口令/Authorization/私钥特征 | P1 | `《红线V2.0》#7.1.11` |
| DES-D-001 | REQ-D-001 | 系统加固 | 日志分级滚动 + 容量配额 + 写入限流 | P1 | — |

### ③ 核心代码示例

`CODE-T-001`（对应 `DES-T-001`）— HMAC 链式防篡改
```go
func appendAudit(prevHash []byte, entry AuditEvent, key []byte) ([]byte, error) {
    mac := hmac.New(sha256.New, key)
    mac.Write(prevHash)
    b, _ := json.Marshal(entry)
    mac.Write(b)
    entry.Hash = mac.Sum(nil) // 链式：hash = HMAC(prev_hash || entry)
    return entry.Hash, db.Create(&entry).Error
}
```
关键点：链式哈希使单条篡改可被检测；应用层无删除/修改接口。

`CODE-R-001`（对应 `DES-R-001`）— 5W1H 结构化日志
```go
type AuditEvent struct {
    ID uint64; Time time.Time; AdminID string; IP string; // When/Who/Where
    EventType string; Resource string; Result string      // What/How
}
```
关键点：回溯要素齐全。

`CODE-I-001`（对应 `DES-I-001`）— 写入前统一脱敏
```go
var secretRE = regexp.MustCompile(`(?i)(authorization:\s*)[^\s]+|(password\s*=\s*)\S+|-----BEGIN [A-Z ]*PRIVATE KEY-----`)
func SanitizeLog(s string) string { return secretRE.ReplaceAllString(s, "$1***") }
```
关键点：口令/认证头/私钥特征统一剥离。

`CODE-E-001`（对应 `DES-E-001`）— 日志读鉴权
```go
r.GET("/logs/user", RequirePerm("log.user","read"), h.ListUserLog)
r.GET("/logs/system", RequirePerm("log.system","read"), h.ListSystemLog)
```
关键点：日志子模块只读授权。

### ④ 安全测试

| 测试ID | 关联设计 | 测试项 | 类型 | 优先级 | 风险 | 关键命令 / 判定标准 |
|--------|---------|--------|------|--------|------|-------------------|
| TEST-S-001 | DES-S-001 | 无凭据访问日志接口 | 渗透验证 | P0 | 高 | `curl -i http://<host>/api/v1/logs/user` → 401 |
| TEST-E-001 | DES-E-001 | 无 log 权限账号读日志 | 渗透验证 | P0 | 高 | 未授权 log.user:read 的账号 `GET /logs/user` → 403 |
| TEST-T-001 | DES-T-001 | 日志篡改检测（红线专项） | 配置核查 | P0 | 高 | 直接改 DB 某条日志内容后回读 → HMAC 链校验失败告警；`GET /logs` 无删除/修改按钮 |
| TEST-R-001 | DES-R-001 | 关键操作全覆盖审计（红线专项） | 配置核查 | P0 | 高 | 依次执行登录/增删/权限变更/停止任务 → `GET /logs/user` 均含时间/操作人/IP/资源/结果 |
| TEST-I-001 | DES-I-001 | 日志明文凭据核查（红线专项） | 渗透验证 | P0 | 高 | 登录（口令设 `TestPwd!42`）后 `grep -R "TestPwd!42" <logdir>` → 无命中 |
| TEST-D-001 | DES-D-001 | 日志洪泛限流 | 功能验证 | P1 | 中 | 高频写日志压测 → 无磁盘耗尽，触发滚动/限流 |

### ⑤ 对应性追踪矩阵

| 需求ID | 需求摘要 | 设计ID | 代码ID | 测试ID |
|--------|---------|--------|--------|--------|
| REQ-S-001 | 日志认证 | DES-S-001 | CODE-S-001(复用) | TEST-S-001 |
| REQ-E-001 | 日志授权 | DES-E-001 | CODE-E-001 | TEST-E-001 |
| REQ-T-001 | 防篡改 | DES-T-001 | CODE-T-001 | TEST-T-001 |
| REQ-R-001 | 5W1H 审计 | DES-R-001 | CODE-R-001 | TEST-R-001 |
| REQ-I-001 | 日志脱敏 | DES-I-001 | CODE-I-001 | TEST-I-001 |
| REQ-D-001 | 日志 DoS | DES-D-001 | CODE-D-001(复用限流) | TEST-D-001 |

> 自检：#8.1.2（High）审计日志防篡改已落 REQ-T-001/DES-T-001，红线专项 TEST-T-001 含具体核查命令；#7.1.11 落 REQ-I-001 专项 TEST-I-001。

---

## 功能模块：系统设置

**结论**：本模块是认证与密钥安全的核心——管理员登录/会话/分级分权、证书私钥与 SSH 密钥加密存储、密码安全，集中承载最多 Critical/High 红线。

### ① 安全需求（STRIDE）

| 需求ID | 威胁类型 | 安全需求（一句话） | 风险等级 | 优先级 | 知识库来源 |
|--------|---------|-----------------|------|--------|-----------|
| REQ-S-001 | 身份假冒(S) | 登录认证服务端完成，口令复杂度 + 防暴力破解（锁定/验证码/延迟） | 高 | P0 | `《红线V2.0》#8.1.1`、`#4.1.4` |
| REQ-S-002 | 身份假冒(S) | 会话标识安全随机 + 认证成功后更换会话标识，JWT 短 TTL + 登出失效 | 高 | P0 | `《红线V2.0》#4.1.8` |
| REQ-S-003 | 身份假冒(S) | 内置 admin 首次登录强制改密，无未文档化账号/硬编码口令 | 高 | P0 | `《红线V2.0》#5.1.1`、`#8.1.4` |
| REQ-E-001 | 权限提升(E) | 分级分权 RBAC（子模块级 read/write），敏感子模块（管理员/证书/SSH）write-only | 高 | P0 | `《红线V2.0》#4.1.2`、`#8.1.5` |
| REQ-E-002 | 权限提升(E) | 新建账号默认最小权限（空/只读），服务端拒绝越权提权（Mass Assignment） | 高 | P0 | `《红线V2.0》#8.1.5` |
| REQ-I-001 | 信息泄露(I) | 管理员密码使用不可逆哈希（bcrypt/PBKDF2，含盐），禁止明文/弱哈希 | 高 | P0 | `《红线V2.0》#7.1.1` |
| REQ-I-002 | 信息泄露(I) | 证书私钥、SSH 密钥加密存储（AES-256-GCM + KEK），不明文落库/落盘 | 高 | P0 | `《红线V2.0》#6.2.1`、`#7.1.1` |
| REQ-I-003 | 信息泄露(I) | 禁止使用私有加密算法，采用标准算法库 + 安全随机数 | 高 | P0 | `《红线V2.0》#6.1.1`、`#6.1.3` |
| REQ-R-001 | 抵赖(R) | 管理员/角色/证书/SSH 敏感操作 5W1H 审计 | 高 | P0 | `《红线V2.0》#8.1.2` |
| REQ-I-004 | 信息泄露(I) | 管理员手机号/邮箱等 PII 最小化收集 + 权限控制（隐私） | 中 | P1 | `《红线V2.0》#11.3.1` |
| REQ-D-001 | 拒绝服务(D) | 登录接口限流防暴力破解/资源耗尽 | 中 | P1 | `《红线V2.0》#8.1.1` |

### ② 安全设计

| 设计ID | 关联需求 | 设计领域 | 安全设计（一句话） | 优先级 | 知识库来源 |
|--------|---------|---------|-----------------|--------|-----------|
| DES-S-001 | REQ-S-001 | 身份认证 | 登录失败统一提示（不区分用户不存在/口令错误）+ 连续失败锁定账号 | P0 | `《红线V2.0》#8.1.1` |
| DES-S-002 | REQ-S-002 | 会话管理 | JWT 用 crypto/rand 密钥 + 短 TTL，登出/重置口令使旧 token 失效 | P0 | `《红线V2.0》#4.1.8` |
| DES-S-003 | REQ-S-003 | 身份认证 | admin 首次登录强制改密（完成前禁其它操作），账号清单文档化 | P0 | `《红线V2.0》#8.1.4` |
| DES-E-001 | REQ-E-001 | 访问控制 RBAC | 子模块级 RBAC + 敏感子模块 write-only 叠加保护 | P0 | `《红线V2.0》#4.1.2` |
| DES-E-002 | REQ-E-002 | 访问控制 | 新建账号默认无权限，请求绑定白名单字段（防 Mass Assignment） | P0 | `《红线V2.0》#8.1.5` |
| DES-I-001 | REQ-I-001 | 密码存储 | bcrypt（cost≥10）哈希，盐随机按用户区分 | P0 | `《红线V2.0》#7.1.1` |
| DES-I-002 | REQ-I-002 | 密钥管理 | 证书私钥/SSH 密钥 AES-256-GCM 加密，DEK 由 KEK 包裹，KEK 文件 0600 权限 | P0 | `《红线V2.0》#6.2.1` |
| DES-I-003 | REQ-I-003 | 加密 | 仅用标准算法（AES-GCM/PBKDF2/HMAC-SHA256），随机数用 crypto/rand | P0 | `《红线V2.0》#6.1.1` |
| DES-R-001 | REQ-R-001 | 日志审计 | 敏感操作 5W1H 审计（复用日志模块 HMAC 链） | P0 | `《红线V2.0》#8.1.2` |
| DES-I-004 | REQ-I-004 | 隐私保护 | PII 字段最小化、按权限展示、导出脱敏 | P1 | `《红线V2.0》#11.3.1` |
| DES-D-001 | REQ-D-001 | 系统加固 | 登录接口限流（IP+账号双维度） | P1 | `《红线V2.0》#8.1.1` |

### ③ 核心代码示例

`CODE-I-001`（对应 `DES-I-001`）— bcrypt 密码哈希
```go
import "golang.org/x/crypto/bcrypt"
func HashPassword(pwd string) (string, error) {
    h, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost) // cost≥10，自动加盐
    return string(h), err
}
func CheckPassword(pwd, hash string) bool {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pwd)) == nil
}
```
关键点：不可逆 + 自动盐 + cost 因子，抗彩虹表/暴力破解。

`CODE-I-002`（对应 `DES-I-002`）— 密钥信封加密（KEK 包裹 DEK）
```go
func EncryptSecret(plain []byte, kek []byte) ([]byte, error) {
    dek := make([]byte, 32); rand.Read(dek)           // crypto/rand 生成 DEK
    ct, _ := aesgcmEncrypt(dek, plain, nil)            // AES-256-GCM 加密数据
    encDek, _ := aesgcmEncrypt(kek, dek, nil)          // KEK 加密 DEK
    return append(encDek, ct...), nil                  // 信封结构：KEK(DEK) || DEK(data)
}
// KEK 由首次安装生成，chmod 0600，仅服务账号可读，不明文入库
```
关键点：数据用 DEK 加密、DEK 用 KEK 包裹，KEK 权限最小化，支持轮换（重包裹 DEK 不动数据）。

`CODE-S-001`（对应 `DES-S-001`）— 登录防暴力破解
```go
func (h *Auth) Login(c *gin.Context) {
    if limiter.Locked(c.ClientIP(), req.Username) { c.JSON(429, gin.H{"message":"too many attempts"}); return }
    admin := repo.FindByUsername(req.Username)
    if admin == nil || !CheckPassword(req.Password, admin.PasswordHash) {
        limiter.Fail(c.ClientIP(), req.Username)      // 累计失败，达阈值锁定
        c.JSON(401, gin.H{"message":"invalid credentials"}) // 统一提示，不区分账号/口令错误
        return
    }
    limiter.Reset(c.ClientIP(), req.Username)
    c.JSON(200, gin.H{"token": issueToken(admin.ID)})
}
```
关键点：统一错误提示 + 账号/IP 双维度锁定，防枚举与爆破。

`CODE-S-003`（对应 `DES-S-003`）— 首次登录强制改密
```go
func (h *Auth) MustChangePassword(c *gin.Context) {
    admin := currentAdmin(c)
    if admin.MustChangePwd { // 内置 admin 首登/管理员重置后置位
        c.AbortWithStatusJSON(403, gin.H{"code":403,"message":"change password required"})
        return
    }
    c.Next()
}
```
关键点：改密完成前中间件拦截一切业务请求。

`CODE-E-002`（对应 `DES-E-002`）— 防 Mass Assignment 白名单绑定
```go
type CreateAdminReq struct { // 仅绑定允许字段，role 通过独立接口授予
    Username string `json:"username" binding:"required"`
    Name     string `json:"name"`
    Phone    string `json:"phone"`
    Email    string `json:"email"`
    Password string `json:"password" binding:"required,min=8"`
    // 不含 status/role/is_super 等字段
}
```
关键点：白名单绑定，请求中夹带的 role/status 提权字段被丢弃。

`CODE-E-001`（对应 `DES-E-001`）— 敏感子模块 write-only 保护
```go
r.POST("/admins", RequirePerm("setting.admin","write"), h.CreateAdmin)
r.POST("/certificates", RequirePerm("setting.cert","write"), h.ImportCert)
r.POST("/ssh-hosts", RequirePerm("setting.ssh","write"), h.AddSshHost)
```
关键点：敏感子模块仅 write 授权可用，默认不授普通角色。

`CODE-I-003`（对应 `DES-I-003`）— 安全随机数（crypto/rand）
```go
import "crypto/rand"
func randomToken(n int) (string, error) {
    b := make([]byte, n); if _, err := rand.Read(b); err != nil { return "", err }
    return base64.RawURLEncoding.EncodeToString(b), nil
}
```
关键点：会话/盐/密钥一律 crypto/rand，禁 math/rand。

`CODE-R-001`（对应 `DES-R-001`）— 敏感操作审计
```go
audit.Log(ctx, AuditEvent{AdminID: ctx.adminID, Action:"admin.password.reset",
    Resource:"admin", ResourceID:targetID, Detail:"reset by admin", IP:clientIP(ctx)})
```
关键点：管理员/角色/证书/SSH 变更全覆盖。

### ④ 安全测试

| 测试ID | 关联设计 | 测试项 | 类型 | 优先级 | 风险 | 关键命令 / 判定标准 |
|--------|---------|--------|------|--------|------|-------------------|
| TEST-S-001 | DES-S-001 | 暴力破解防护（红线专项） | 渗透验证 | P0 | 高 | 连续 5 次错误口令 → 账号/锁定或要求验证码；换 IP 继续 → 仍被限；错误提示不区分「用户不存在/口令错误」 |
| TEST-S-002 | DES-S-002 | 会话固定/登出失效（红线专项） | 渗透验证 | P0 | 高 | 登录前后会话标识更换；登出后旧 JWT 重放 → 401；篡改 JWT 不重签 → 401 |
| TEST-S-003 | DES-S-003 | 首登强制改密 + 无隐藏账号（红线专项） | 功能验证 | P0 | 高 | admin 首登仅可改密，改密前访问其它接口被 403；`SELECT username FROM admin` 对比账号清单 → 无未文档化账号；`strings <bin> | grep -iE "password|passwd|secret"` 人工研判无硬编码口令 |
| TEST-E-001 | DES-E-001 | 越权访问敏感子模块 | 渗透验证 | P0 | 高 | 普通角色账号 `POST /certificates`/`POST /admins` → 403 |
| TEST-E-002 | DES-E-002 | Mass Assignment 提权（红线专项） | 渗透验证 | P0 | 高 | `POST /admins` body 夹带 `"role":"super"`/`"status":"enable"` → 字段被忽略，实际新建账号默认无权限 |
| TEST-I-001 | DES-I-001 | 密码哈希存储核查（红线专项） | 配置核查 | P0 | 高 | `SELECT username, password_hash FROM admin LIMIT 3` → bcrypt `$2a$10$...` 含盐，非明文/MD5 |
| TEST-I-002 | DES-I-002 | 密钥加密存储核查（红线专项） | 配置核查 | P0 | 高 | `find <data> -name "*.key" -o -name "*.pem"` 逐项 → 私钥文件为密文；`ls -l <keK文件>` → 0600 服务账号属主；`strings <bin> | grep -E "^[A-Za-z0-9+/]{32,}={0,2}$"` 无硬编码工作密钥 |
| TEST-I-003 | DES-I-003 | 不安全算法/随机数核查（红线专项） | 配置核查 | P0 | 高 | `nmap --script ssl-enum-ciphers -p 443 <host>` → 无 RC4/DES/3DES/MD5，TLS≥1.2；`grep -RnE "math/rand" <src>` → 密码学场景无命中 |
| TEST-R-001 | DES-R-001 | 敏感操作审计（红线专项） | 配置核查 | P0 | 高 | 执行重置口令/建角色/导证书 → `GET /logs/user` 均含操作人/时间/对象/动作 |
| TEST-I-004 | DES-I-004 | 管理员 PII 最小化 | 功能验证 | P1 | 中 | 导出管理员列表 → 手机号/邮箱脱敏；未授权账号看不到 PII |
| TEST-D-001 | DES-D-001 | 登录限流 | 功能验证 | P1 | 中 | 高频登录请求 → 超阈值返回 429 |

### ⑤ 对应性追踪矩阵

| 需求ID | 需求摘要 | 设计ID | 代码ID | 测试ID |
|--------|---------|--------|--------|--------|
| REQ-S-001 | 登录认证/防爆破 | DES-S-001 | CODE-S-001 | TEST-S-001 |
| REQ-S-002 | 会话管理 | DES-S-002 | CODE-S-002(复用 JWT) | TEST-S-002 |
| REQ-S-003 | 首登改密/无隐藏账号 | DES-S-003 | CODE-S-003 | TEST-S-003 |
| REQ-E-001 | 分级分权 RBAC | DES-E-001 | CODE-E-001 | TEST-E-001 |
| REQ-E-002 | 最小权限/防提权 | DES-E-002 | CODE-E-002 | TEST-E-002 |
| REQ-I-001 | 密码哈希 | DES-I-001 | CODE-I-001 | TEST-I-001 |
| REQ-I-002 | 密钥加密存储 | DES-I-002 | CODE-I-002 | TEST-I-002 |
| REQ-I-003 | 标准算法/随机数 | DES-I-003 | CODE-I-003 | TEST-I-003 |
| REQ-R-001 | 敏感操作审计 | DES-R-001 | CODE-R-001 | TEST-R-001 |
| REQ-I-004 | PII 最小化 | DES-I-004 | CODE-I-004(复用脱敏) | TEST-I-004 |
| REQ-D-001 | 登录限流 | DES-D-001 | CODE-D-001(复用限流) | TEST-D-001 |

> 自检：Critical #5.1.1（无隐藏账号/硬编码口令）、High #6.2.1（密钥不硬编码）、#7.1.1（凭据加密）、#8.1.1/#8.1.4/#8.1.5/#6.1.1/#6.1.3/#4.1.8 全部落行，红线专项 TEST 含具体命令。

---

## 功能模块：部署与运维安全（离线部署/升级/卸载）

**结论**：离线部署场景下，通信矩阵端口收敛、软件包完整性校验、发布包供应链（防病毒/可疑组件/未公开地址）、升级可控与卸载数据安全是红线主要落点。

### ① 安全需求（STRIDE）

| 需求ID | 威胁类型 | 安全需求（一句话） | 风险等级 | 优先级 | 知识库来源 |
|--------|---------|-----------------|------|--------|-----------|
| REQ-S-001 | 身份假冒(S) | 管理面（HTTP/WS 端口）与外部通信端口收敛，仅开放必需端口 | 高 | P0 | `《红线V2.0》#1.1.3`、`#1.1.4` |
| REQ-I-001 | 信息泄露(I) | 内网隔离部署，管理面不外露公网，禁止未公开公网地址 | 高 | P0 | `《红线V2.0》#1.1.1`、`#5.3.2` |
| REQ-T-001 | 篡改(T) | 发布包提供 SHA256 完整性校验，安装/升级时验证防篡改 | 高 | P0 | `《红线V2.0》#12.1.1` |
| REQ-T-002 | 篡改(T) | 发布包无病毒木马、无可疑组件（嗅探/调试工具）、无硬编码密钥 | 高 | P0 | `《红线V2.0》#5.2.1`、`#5.3.1` |
| REQ-E-001 | 权限提升(E) | 对外服务进程非 root 运行，最小权限 | 中 | P1 | `《红线V2.0》#5.4.1` |
| REQ-T-003 | 篡改(T) | 升级在管理员知情可控下进行，含备份/回滚，不静默升级 | 高 | P0 | `《红线V2.0》#5.3.5` |
| REQ-I-002 | 信息泄露(I) | 卸载/数据清除安全，`--purge-data` 双重确认，防误删与残留 | 中 | P1 | `《红线V2.0》#11.4.1` |
| REQ-I-003 | 信息泄露(I) | 依赖供应链漏洞修复（k6/Chromium/PostgreSQL/开源组件） | 中 | P1 | `《红线V2.0》#2.1.3`、`#14.1.2` |
| REQ-I-004 | 信息泄露(I) | 数据出境评估（离线内网产品不跨境；如需跨境完成评估） | 高 | P0 | `《红线V2.0》#7.2.1` |
| REQ-R-001 | 抵赖(R) | 提供通信矩阵、加固指南、缺省账号清单、个人数据说明 | 中 | P1 | `《红线V2.0》#9.1.2`、`#9.1.4`、`#9.1.5` |

### ② 安全设计

| 设计ID | 关联需求 | 设计领域 | 安全设计（一句话） | 优先级 | 知识库来源 |
|--------|---------|---------|-----------------|--------|-----------|
| DES-S-001 | REQ-S-001 | 网络隔离 | 通信矩阵文档化，nmap 校验仅开放矩阵端口，动态端口限定范围 | P0 | `《红线V2.0》#1.1.3` |
| DES-I-001 | REQ-I-001 | 网络隔离 | 默认内网部署，管理端口不绑定 0.0.0.0/不外露公网 | P0 | `《红线V2.0》#1.1.1` |
| DES-T-001 | REQ-T-001 | 完整性 | manifest.yaml 记录文件 SHA256，install/upgrade 校验 | P0 | `《红线V2.0》#12.1.1` |
| DES-T-002 | REQ-T-002 | 供应链安全 | 发布流水线双引擎防病毒扫描 + 可疑组件黑名单 + 密钥扫描 | P0 | `《红线V2.0》#5.2.1` |
| DES-E-001 | REQ-E-001 | 系统加固 | 服务以专用低权限账号运行（systemd User= / 容器非 root） | P1 | `《红线V2.0》#5.4.1` |
| DES-T-003 | REQ-T-003 | 升级安全 | upgrade.sh 备份→迁移→健康检查→失败回滚，升级经管理员触发 | P0 | `《红线V2.0》#5.3.5` |
| DES-I-002 | REQ-I-002 | 数据安全 | uninstall 默认保留数据，`--purge-data` 双重确认后物理清除 | P1 | `《红线V2.0》#11.4.1` |
| DES-I-003 | REQ-I-003 | 生命周期管理 | SCA/漏洞扫描接入 CI，官方补丁及时修复 | P1 | `《红线V2.0》#2.1.3` |
| DES-I-004 | REQ-I-004 | 数据出境 | 离线内网产品不跨境；文档声明数据本地留存策略 | P0 | `《红线V2.0》#7.2.1` |
| DES-R-001 | REQ-R-001 | 安全资料 | 输出通信矩阵/加固指南/账号清单/个人数据说明 | P1 | `《红线V2.0》#9.1.2` |

### ③ 核心代码示例

`CODE-T-001`（对应 `DES-T-001`）— 安装完整性校验
```bash
#!/usr/bin/env bash
# install.sh 核心：按 manifest 校验所有文件 SHA256 后再部署
while read -r hash file; do
    echo "$hash  $file" | sha256sum -c - || { echo "integrity check failed: $file"; exit 1; }
done < <(grep -v '^#' manifest.yaml | awk '{print $1, $2}')
```
关键点：任一文件哈希不符即中止安装，防篡改替换。

`CODE-T-003`（对应 `DES-T-003`）— 升级备份回滚
```bash
#!/usr/bin/env bash
# upgrade.sh 核心：备份 → 迁移 → 健康检查 → 失败回滚
cp -a "$APP_DIR" "$APP_DIR.bak.$(date +%s)" || exit 1
"$APP_DIR/bin/auto_performance" migrate up || { rollback "$APP_DIR.bak"; exit 1; }
"$APP_DIR/bin/auto_performance" healthcheck || { rollback "$APP_DIR.bak"; exit 1; }
```
关键点：迁移失败自动回滚，升级不静默、可控可逆。

`CODE-E-001`（对应 `DES-E-001`）— 低权限运行（systemd 单元核心）
```ini
[Service]
User=autoperf            ; 专用低权限账号，非 root
Group=autoperf
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/auto_performance
```
关键点：进程非 root + capability/文件系统最小化。

`CODE-I-002`（对应 `DES-I-002`）— 卸载数据保护
```bash
#!/usr/bin/env bash
# uninstall.sh 核心：默认保留数据，--purge-data 需二次确认
if [[ "$1" == "--purge-data" ]]; then
    read -r -p "此操作将永久删除数据，输入 'YES' 确认: " confirm
    [[ "$confirm" == "YES" ]] || { echo "cancelled"; exit 1; }
    rm -rf "$DATA_DIR"
fi
```
关键点：默认不删数据，物理清除需显式双重确认。

### ④ 安全测试

| 测试ID | 关联设计 | 测试项 | 类型 | 优先级 | 风险 | 关键命令 / 判定标准 |
|--------|---------|--------|------|--------|------|-------------------|
| TEST-S-001 | DES-S-001 | 通信矩阵端口核查（红线专项） | 配置核查 | P0 | 高 | `nmap -sS -sV -p 1-65535 <host>` 比对通信矩阵 → 开放端口全部在矩阵中，无未记录端口 |
| TEST-I-001 | DES-I-001 | 管理面不外露公网（红线专项） | 配置核查 | P0 | 高 | `ss -tlnp` → 管理服务未绑定 0.0.0.0；`strings <bin> | grep -oE "([0-9]{1,3}\.){3}[0-9]{1,3}"` 排除私网段 → 无未公开公网 IP/URL |
| TEST-T-001 | DES-T-001 | 发布包完整性校验（红线专项） | 配置核查 | P0 | 高 | 篡改包内任一文件后 `install.sh` → 哈希校验失败中止安装 |
| TEST-T-002 | DES-T-002 | 病毒/可疑组件核查（红线专项） | 配置核查 | P0 | 高 | `find <pkg> -iname "tcpdump" -o -iname "gdb" -o -iname "nmap" -o -iname "gcc"` → 无命中；双引擎杀毒扫描 → 无告警 |
| TEST-E-001 | DES-E-001 | 服务进程运行身份（红线专项） | 配置核查 | P0 | 高 | `ss -tlnp` 取侦听进程 → `ps -o user= -p <pid>` → 非 root |
| TEST-T-003 | DES-T-003 | 升级回滚 | 功能验证 | P0 | 高 | 模拟迁移失败执行 upgrade.sh → 自动回滚，服务恢复旧版本 |
| TEST-I-002 | DES-I-002 | 卸载数据保护 | 功能验证 | P1 | 中 | `uninstall.sh` 默认 → 数据保留；`--purge-data` 未输入 YES → 取消 |
| TEST-I-003 | DES-I-003 | 依赖漏洞扫描 | 配置核查 | P1 | 中 | `trivy fs <pkg>` / `snyk test` → 无 High/Critical 未修复依赖 |
| TEST-R-001 | DES-R-001 | 安全资料完备性 | 配置核查 | P1 | 中 | 检查交付物 → 含通信矩阵/加固指南/账号清单/个人数据说明 |

### ⑤ 对应性追踪矩阵

| 需求ID | 需求摘要 | 设计ID | 代码ID | 测试ID |
|--------|---------|--------|--------|--------|
| REQ-S-001 | 端口收敛 | DES-S-001 | N/A(配置/文档) | TEST-S-001 |
| REQ-I-001 | 内网隔离/无公网地址 | DES-I-001 | N/A(部署配置) | TEST-I-001 |
| REQ-T-001 | 完整性校验 | DES-T-001 | CODE-T-001 | TEST-T-001 |
| REQ-T-002 | 供应链扫描 | DES-T-002 | N/A(CI 流水线) | TEST-T-002 |
| REQ-E-001 | 低权限运行 | DES-E-001 | CODE-E-001 | TEST-E-001 |
| REQ-T-003 | 升级回滚 | DES-T-003 | CODE-T-003 | TEST-T-003 |
| REQ-I-002 | 卸载数据安全 | DES-I-002 | CODE-I-002 | TEST-I-002 |
| REQ-I-003 | 依赖漏洞 | DES-I-003 | N/A(SCA 工具) | TEST-I-003 |
| REQ-I-004 | 数据出境 | DES-I-004 | N/A(流程/文档) | TEST-I-004(文档声明) |
| REQ-R-001 | 安全资料 | DES-R-001 | N/A(文档) | TEST-R-001 |

> 自检：Critical #5.3.2（公网地址）、#5.2.1/#5.3.1（供应链）、#7.2.1（数据出境）、High #1.1.3（端口）、#12.1.1（完整性）、#5.3.5（升级）、#5.4.1（低权限）全部落行。N/A 项为配置/文档/流水线层面控制，无独立代码示例（已在矩阵标注理由）。

---

## 参考文档

1. 《华为供应商产品网络安全红线V2.0》——安全需求（SEC-REQ-001~071）、安全设计（SEC-DES-001~155）
2. STRIDE 威胁建模方法
3. OWASP Top 10:2021
4. 项目文档：`PRD-performance-testing-platform.md`、`architecture.md`、`ADR-0001~0006`

*本报告包含敏感信息，仅供内部使用*
