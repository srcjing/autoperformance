# auto_performance 错误码维护规范（errcode.md）

- **项目**：auto_performance（性能测试平台）
- **文档类型**：详细设计 / 错误码分层与维护规范
- **状态**：评审中（Draft）
- **日期**：2026-10-26
- **作者**：架构师
- **关联**：
  - 契约：`specs/api-contract.md` §1.5 / §4（错误码表，本规范与其对齐）
  - 机器化：`.project/api/openapi.json`（各 operation 的 `x-err-codes` 引用本规范定义的码值）
  - 架构：`specs/architecture.md`（中间件链、统一响应）

> 本规范定义错误码的**分层边界、段位分配、完整清单、代码组织与扩展机制**，目标是让「全局通用错误」与「业务模块错误」彻底解耦：新增业务错误码**不修改**全局文件、不影响全局错误码的稳定性。

---

## 1. 设计目标与分层原则

### 1.1 目标

1. **单一事实源**：错误码只在后端 `errcode` 包中定义一次，契约文档（`api-contract.md`）与 OpenAPI（`x-err-codes`）均从代码/本规范对齐，不重复维护。
2. **解耦可扩展**：全局通用错误码（`1xxxx`）与业务错误码（`2xxxx`）分层隔离；新增业务错误码通过模块注册机制完成，不触碰全局文件。
3. **可追踪**：每个错误码可回溯到「所属层 + 模块 + HTTP 状态码 + 含义 + 触发场景」，前端可据此做全局拦截与业务分支。

### 1.2 分层边界

| 层 | 段位 | 归属 | 变更频率 | 维护方式 |
| --- | --- | --- | --- | --- |
| 全局通用错误码 | `0` + `1xxxx` | 框架级：认证、鉴权、参数、限流、服务端等 | 极低 | 仅框架/中间件维护，业务禁止新增 |
| 业务错误码 | `2xxxx` | 各业务模块（认证/环境/产品/测试/报告/日志/设置） | 中高 | 各模块自维护，通过注册机制扩展 |

**核心原则**：

- **「能复用全局则不复用业务」**：参数、鉴权、资源不存在、并发冲突、限流、服务端异常等横切问题一律用 `1xxxx`，业务不重复造码。
- **「业务码不跨界」**：一个业务码只属于一个模块段位（如 `21xxx` 只属于环境模块），模块之间不互相引用对方码值。
- **「码值永久保留」**：错误码一经发布永不删除/复用，废弃只做标记（`@deprecated`），保证日志/告警历史可回溯。

---

## 2. 段位分配总表

5 位数字，`code` 前缀与 HTTP 状态码语义一致（见 §5）。

### 2.1 全局通用（1xxxx）

| 段位 | 含义 | HTTP 语义 |
| --- | --- | --- |
| `0` | 成功 | 200 |
| `100xx` | 参数错误/缺失/格式非法 | 400 |
| `101xx` | 认证（未认证/会话失效/需改密） | 401/403 |
| `102xx` | 鉴权（权限不足/账号停用） | 403 |
| `103xx` | 资源（不存在/已存在） | 404/400 |
| `104xx` | 冲突（状态/并发/引用/占用） | 409 |
| `105xx` | 限流（频率/登录锁定） | 429 |
| `106xx` | 服务端（内部错误/依赖异常） | 500 |

### 2.2 业务（2xxxx）

| 段位 | 模块 | 段内段位 |
| --- | --- | --- |
| `200xx` | 认证（auth） | 20001~20099 |
| `210xx` | 环境（environment） | 21001~21099 |
| `220xx` | 产品（product） | 22001~22099 |
| `230xx` | 测试（testing） | 23001~23099 |
| `240xx` | 报告（report） | 24001~24099 |
| `250xx` | 日志（log） | 25001~25099 |
| `260xx` | 设置（system） | 26001~26099 |

> 每个模块预留 100 个码位（`xx001~xx099`），段位内按「资源/动作」细分，见各模块小节。新增模块可申请 `27xxx`、`28xxx`…… 依次递增，避免与既有模块冲突。

---

## 3. 全局通用错误码清单（1xxxx）

| code | HTTP | 含义 | 典型场景 |
| --- | --- | --- | --- |
| 0 | 200 | 成功 | 正常返回 |
| 10001 | 400 | 参数错误 | 参数格式/取值非法 |
| 10002 | 400 | 参数缺失 | 必填字段缺失 |
| 10003 | 400 | 参数格式非法 | 类型/枚举/分页上限（如 page_size>200） |
| 10101 | 401 | 未认证 | 缺少/无效/过期 token |
| 10102 | 401 | 会话失效 | 登出后 token 重放 |
| 10103 | 403 | 需修改密码 | 首登强制改密未完成（除改密/登出/查自身外全部拦截） |
| 10201 | 403 | 权限不足 | RBAC 拒绝 |
| 10202 | 403 | 账号已停用 | 停用管理员登录/访问 |
| 10301 | 404 | 资源不存在 | 目标对象不存在 |
| 10302 | 400 | 资源已存在 | 唯一约束冲突（编码/用户名/标签名） |
| 10401 | 409 | 状态冲突 | 当前状态不允许该操作 |
| 10402 | 409 | 并发冲突 | 乐观锁 version 冲突 |
| 10403 | 409 | 资源被引用 | 存在关联禁止删除 |
| 10404 | 409 | 环境被占用 | 环境互斥（同环境单任务） |
| 10501 | 429 | 请求过于频繁 | 限流 |
| 10502 | 429 | 登录锁定 | 连续失败锁定（账号/IP 双维度） |
| 10601 | 500 | 服务器内部错误 | 未捕获异常 |
| 10602 | 500 | 依赖服务异常 | k6/Chromium/DB 异常 |

---

## 4. 业务错误码清单（2xxxx）

### 4.1 认证 auth（200xx）

| code | HTTP | 含义 |
| --- | --- | --- |
| 20001 | 401 | 用户名或密码错误（统一提示，不区分账号不存在/口令错误） |
| 20002 | 401 | 账号被锁定 |
| 20003 | 400 | 旧密码错误 |
| 20004 | 400 | 新密码不符合复杂度要求（≥8 位，含大小写/数字/特殊字符） |
| 20005 | 400 | 新密码与旧密码相同 |

### 4.2 环境 environment（210xx）

| code | HTTP | 含义 |
| --- | --- | --- |
| 21001 | 400 | 环境编码已存在 |
| 21002 | 409 | 分类已被环境引用，禁止删除 |
| 21003 | 409 | 标签已被环境引用，删除前需解除关联 |
| 21004 | 409 | 环境被任务占用，禁止删除/停用 |

### 4.3 产品 product（220xx）

| code | HTTP | 含义 |
| --- | --- | --- |
| 22001 | 400 | 产品编码已存在 |
| 22002 | 409 | 产品下存在接口，禁止删除 |
| 22003 | 400 | 阈值方向与指标类型不匹配（如 success_rate 配 le） |

### 4.4 测试 testing（230xx）

| code | HTTP | 含义 |
| --- | --- | --- |
| 23001 | 409 | 任务状态不允许该操作 |
| 23002 | 409 | 环境被占用/正在测试（环境互斥） |
| 23003 | 400 | 接口范围为空 |
| 23004 | 400 | 并发数非法或超出上限（max_vus） |
| 23005 | 404 | 模板不存在 |

### 4.5 报告 report（240xx）

| code | HTTP | 含义 |
| --- | --- | --- |
| 24001 | 404 | 报告文件不存在 |
| 24002 | 400 | 上传文件类型不允许（扩展名+magic bytes 校验失败） |
| 24003 | 400 | 上传文件过大（max_report_size） |
| 24004 | 500 | 报告生成失败 |

### 4.6 日志 log（250xx）

| code | HTTP | 含义 |
| --- | --- | --- |
| 25001 | 400 | 查询时间范围非法 |

### 4.7 设置 system（260xx）

| code | HTTP | 含义 |
| --- | --- | --- |
| 26001 | 400 | 用户名已存在 |
| 26002 | 403 | 内置 admin 不可删除/停用 |
| 26003 | 409 | 角色被管理员引用，禁止删除 |
| 26004 | 403 | 内置自签证书不可删除 |
| 26005 | 409 | SSH/Host 已被环境引用，禁止删除 |

---

## 5. 错误码与 HTTP 状态码对齐约定

统一响应体 `{code, message, data}`，`code` 前缀与 HTTP 状态码语义一致，前端双轨拦截：

| code 段 | HTTP | 前端全局处理 |
| --- | --- | --- |
| `0` | 200 | 正常 |
| `100xx` | 400 | 表单/参数提示 |
| `101xx` | 401（10103 为 403） | 跳登录（10101/10102）；强制改密页（10103） |
| `102xx` | 403 | 403 无权限页（10201）；账号停用提示（10202） |
| `103xx` | 404/400 | 资源不存在/已存在提示 |
| `104xx` | 409 | 冲突提示（状态/并发/引用/占用） |
| `105xx` | 429 | 限流提示（10501）；登录锁定（10502） |
| `106xx` | 500 | 服务端错误兜底 |
| `2xxxx` | 见各清单（多为 400/403/404/409） | 业务分支提示（以 `code` 为准） |

> 约定：**前端以 HTTP 状态码做全局拦截（跳登录/403），以 `code` 做业务分支**。服务端返回错误时必须同时给出正确的 HTTP 状态码与 `code`，二者不得错位（如业务码不得配 200）。

---

## 6. 代码组织与扩展机制

### 6.1 目录结构

```
internal/errcode/
├── code.go          # 全局通用错误码（1xxxx）+ 核心类型 Error + 注册表
├── http.go          # code → HTTP 状态码映射（全局 + 注册时注入）
└── registry.go      # Register()/ByCode()/All() 注册与查询入口

internal/module/auth/errors.go        # 认证业务错误码（200xx）
internal/module/environment/errors.go # 环境业务错误码（210xx）
internal/module/product/errors.go     # 产品业务错误码（220xx）
internal/module/testing/errors.go     # 测试业务错误码（230xx）
internal/module/report/errors.go      # 报告业务错误码（240xx）
internal/module/log/errors.go         # 日志业务错误码（250xx）
internal/module/system/errors.go      # 设置业务错误码（260xx）
```

### 6.2 核心类型与注册机制（伪代码，Go）

```go
// internal/errcode/code.go

package errcode

// Error 统一错误模型：code + http 状态码 + 含义。
type Error struct {
    Code    int    // 5 位数字，全局 1xxxx / 业务 2xxxx
    HTTP    int    // HTTP 状态码，与 code 前缀语义一致
    Message string // 人类可读含义
}

func (e *Error) Error() string { return e.Message }

// 全局通用错误码（框架级，业务禁止新增）。
var (
    OK            = &Error{0, 200, "ok"}
    ErrParam      = &Error{10001, 400, "参数错误"}
    ErrParamMiss  = &Error{10002, 400, "参数缺失"}
    ErrParamFmt   = &Error{10003, 400, "参数格式非法"}
    ErrUnauth     = &Error{10101, 401, "未认证"}
    ErrSession    = &Error{10102, 401, "会话失效"}
    ErrMustPwd    = &Error{10103, 403, "需修改密码"}
    ErrForbidden  = &Error{10201, 403, "权限不足"}
    ErrDisabled   = &Error{10202, 403, "账号已停用"}
    ErrNotFound   = &Error{10301, 404, "资源不存在"}
    ErrConflict   = &Error{10302, 400, "资源已存在"}
    ErrState      = &Error{10401, 409, "状态冲突"}
    ErrConcurrent = &Error{10402, 409, "并发冲突"}
    ErrReferenced = &Error{10403, 409, "资源被引用"}
    ErrEnvBusy    = &Error{10404, 409, "环境被占用"}
    ErrRateLimit  = &Error{10501, 429, "请求过于频繁"}
    ErrLoginLock  = &Error{10502, 429, "登录锁定"}
    ErrInternal   = &Error{10601, 500, "服务器内部错误"}
    ErrDependency = &Error{10602, 500, "依赖服务异常"}
)
```

```go
// internal/errcode/registry.go

package errcode

import (
    "fmt"
    "sync"
)

// 业务错误码注册表：模块通过 Register 扩展，不修改全局文件。
var (
    registryMu sync.RWMutex
    registry   = map[int]*Error{}
)

// Register 注册一个业务错误码（2xxxx）。重复注册/越界段位将 panic，保证码值唯一。
func Register(e *Error) *Error {
    registryMu.Lock()
    defer registryMu.Unlock()
    if e.Code < 20000 || e.Code > 29999 {
        panic(fmt.Sprintf("errcode: 业务码越界 %d", e.Code))
    }
    if _, dup := registry[e.Code]; dup {
        panic(fmt.Sprintf("errcode: 重复注册 %d", e.Code))
    }
    registry[e.Code] = e
    return e
}

// ByCode 按码值查询（全局优先，其次业务注册表）。
func ByCode(code int) *Error {
    if e := globalByCode(code); e != nil {
        return e
    }
    registryMu.RLock()
    defer registryMu.RUnlock()
    return registry[code]
}
```

```go
// internal/module/environment/errors.go —— 业务模块扩展示例

package environment

import "auto_performance/internal/errcode"

// 模块业务错误码：通过 errcode.Register 注册，不影响全局文件。
var (
    ErrCodeExists       = errcode.Register(&errcode.Error{21001, 400, "环境编码已存在"})
    ErrCategoryRefed    = errcode.Register(&errcode.Error{21002, 409, "分类已被环境引用，禁止删除"})
    ErrTagRefed         = errcode.Register(&errcode.Error{21003, 409, "标签已被环境引用，删除前需解除关联"})
    ErrEnvOccupied      = errcode.Register(&errcode.Error{21004, 409, "环境被任务占用，禁止删除/停用"})
)
```

### 6.3 「新增业务错误码不修改全局文件」的机制

1. **隔离**：全局 `1xxxx` 定义在 `internal/errcode/code.go`，业务 `2xxxx` 定义在各自模块的 `errors.go`，二者物理隔离。
2. **注册制**：业务模块通过 `errcode.Register(...)` 把本模块错误码写入集中注册表，运行时按 `code` 查询；模块只依赖「注册接口」，不依赖全局文件内容。
3. **段位校验**：`Register` 强制 `2xxxx` 段位，并做重复注册检测，杜绝跨模块码值冲突。
4. **扩展流程**：新增业务错误码 = 在所属模块 `errors.go` 增加一行 `Register` 定义 → 更新本规范 §4 对应模块清单 → 更新 `openapi.json` 相关接口的 `x-err-codes`。**全局文件零改动**。
5. **新模块**：新增业务模块时申请新段位（如 `270xx`），在 `errcode` 注册表中仅声明段位归属，不修改已有全局码值。

### 6.4 错误码生命周期

- **新增**：按 §6.3 流程，码值从段位内最小值递增，禁止跳号复用。
- **废弃**：不删除，仅标记 `@deprecated` 并指向替代码；文档 §4 保留历史行并标注废弃。
- **变更**：仅允许修改 `Message` 文案（前后端兼容），`Code` 与 `HTTP` 一经发布不可变更。

---

## 7. 与 OpenAPI 的联动

- `openapi.json` 中每个 operation 的 `x-err-codes` 引用本规范定义的码值（全局 + 业务）。
- 全局通用错误码（`10001~10602`）默认适用于**所有**接口，`x-err-codes` 仅标注该接口**特有的关键业务错误码**，避免重复罗列。
- 本规范 §3/§4 是错误码的**唯一权威清单**；OpenAPI 与 `api-contract.md` 均以其为准，新增/变更错误码时同步三处（代码、本规范、OpenAPI）。
