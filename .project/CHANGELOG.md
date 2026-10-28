# 设计变更归档记录

本文件记录项目设计文档的每一次新增、修正、归档与废弃。每次功能实现或变更后必须追加记录。

| 日期 | 类型 | 涉及模块 | 摘要 | 关联文档 | 关联提交 |
| --- | --- | --- | --- | --- | --- |
| 2026-10-26 | 新增 | 全局 | 建立设计文档目录结构与归档/修订约定 | `README.md` | - |
| 2026-10-26 | 新增 | 全模块 | 产出 PRD（6 大模块 + 13 类实体/三套状态机/阈值评估规则/8 假设 13 待确认决策） | `prds/PRD-performance-testing-platform.md` | - |
| 2026-10-26 | 新增 | 架构 | 产出技术选型与架构设计（含项目结构图、前端层级图） | `specs/architecture.md`、`specs/frontend-structure.md` | - |
| 2026-10-26 | 新增 | 架构 | 沉淀 4 项 ADR（后端技术栈/前端技术栈/压测引擎/数据库） | `decisions/ADR-0001~0004` | - |
| 2026-10-26 | 新增 | 前端 | 产出前端高保真原型（自包含静态 HTML） | `prototypes/index.html` | - |
| 2026-10-26 | 修正 | 全局 | 确认 10 项待确认决策：并发互斥、任务/接口快照、报告全格式(HTML+PDF+Word)、孤立归档、分类必填、标签预定义、逻辑删除、分级分权权限、密钥加密、P95+P99+错误率口径 | `prds/PRD-performance-testing-platform.md` | - |
| 2026-10-26 | 修正 | 前端 | 前端原型「系统设置→管理员管理」改为分级分权视图，新增子模块权限矩阵与编辑抽屉 | `prototypes/index.html` | - |
| 2026-10-26 | 修正 | 系统设置 | 补充管理员字段（姓名/手机号/邮箱/描述）、角色授权（角色=权限集合，支持增删改）、证书导入/删除+内置自签证书不可删可重新初始化、SSH/Host 导入/修改/删除 | `prds/PRD-performance-testing-platform.md` | - |
| 2026-10-26 | 修正 | 系统设置 | SSH/Host 增加「添加（新建）」操作，与导入/修改/删除并列 | `prds/PRD-performance-testing-platform.md`、`prototypes/index.html` | - |
| 2026-10-26 | 修正 | 架构 | 架构文档同步 PRD 最新变更：分级分权权限(RBAC+角色)、报告全格式、统计口径、证书/SSH 新字段、数据一致性约定；新增 ADR-0005 权限模型、ADR-0006 报告与统计口径 | `specs/architecture.md`、`specs/frontend-structure.md`、`decisions/ADR-0005~0006` | - |
| 2026-10-26 | 修正 | 测试管理 | 确认测试流程可配置参数（5 阶段 + 压测参数：固定/阶梯/脉冲）、环境清理语义（4 类动作 + 脚本；成功自动清理，失败标记「清理待确认」待管理员确认）；同步任务状态机与架构文档时序图/数据模型 | `prds/PRD-performance-testing-platform.md`、`specs/architecture.md` | - |
| 2026-10-26 | 新增 | 安全 | 产出完整安全需求与设计报告（STRIDE 六维 × 7 模块，REQ→DES→CODE→TEST 全链追溯，华为红线 Critical/High 全覆盖） | `security/security-design-report.md` | - |
| 2026-10-26 | 新增 | 架构 | 产出 REST API 接口契约（78 个接口，覆盖 7 大模块 + 认证，含设计约定/数据流/错误码/权限映射） | `specs/api-contract.md` | - |
| 2026-10-26 | 新增 | 工程化 | 接口机器化：OpenAPI 3.0 单一数据源（78 接口）+ 自包含可视化页面（自动渲染示例）；错误码解耦规范（全局 1xxxx / 业务 2xxxx 分层 + Register 注册机制） | `api/openapi.json`、`api/index.html`、`specs/errcode.md` | - |
| 2026-10-26 | 新增 | 工程 | 搭建工程骨架：Go 后端（分层/错误码 1xxxx/中间件链/统一响应）+ Vue3 前端（路由/store/布局/17 页面占位）+ 离线打包脚本；go build/vet、npm install/build/type-check 均通过 | `backend/`、`frontend/`、`deploy/` | - |
| 2026-10-26 | 新增 | 全局 | 产出实现计划（10 Phase，10 条关键不变量 INV-1~10，核心逻辑 TDD 示例，AC/DFX/红线覆盖索引） | `specs/plan.md` | - |
| 2026-10-26 | 新增 | 实现 | 按 plan.md 完成全部 10 个 Phase 实现（基础设施/认证分权/环境/产品/测试管理/报告/日志/系统设置/部署打包/联调安全加固），后端 go build/vet/test 与前端 type-check/build 全绿；dev 分支 | `specs/plan.md`、`backend/`、`frontend/`、`deploy/` | dev |
| 2026-10-28 | 新增 | 测试管理 | 新增测试模式（单接口 `single` / 流程 `flow`）与流程编排：步骤顺序、接口引用、路径/请求头/请求体覆盖、响应变量提取（JSONPath/正则）；步骤作用域三档（once_per_test / once_per_iteration / every_request）对应登录取凭证、主流程流量、每请求刷新；k6 脚本按作用域生成 setup/主流程/每请求前置；前端新增流程步骤编辑器 | `prds/PRD-performance-testing-platform.md` §3.3.6.1、`specs/api-contract.md` §2.4 | dev |
| 2026-10-28 | 修正 | 测试管理/前端 | 流程编辑体验：可用变量改为可点击 chip（插入光标处）、路径/请求体引用变量高亮、请求头改 Postman 式键值行（接口表单）；新增任务快照同步（待执行任务执行前自动刷新 + 手动 refresh-snapshot），接口配置变更后任务/模板展示与执行同步生效 | `prds/PRD-performance-testing-platform.md`、`specs/api-contract.md` | dev |
| 2026-10-28 | 修正 | 测试管理 | 流程变量排查能力：k6 输出 AFWARN 行，变量未定义/提取失败写入测试日志（去重 + 上限 100）；前端步骤编辑器对未定义变量红色提示；任务详情新增「流程步骤」表（顺序/接口/作用域/提取/引用） | `specs/api-contract.md` | dev |
| 2026-10-28 | 修正 | 测试管理 | 请求详情记录实际发送的请求体：k6 AFREQ 回传渲染后 body（≤4KB），流程步骤级 body 覆盖优先；修复 GET 带 body 被丢弃（改 http.request）；JSON 体未声明 Content-Type 时自动补 application/json | `specs/api-contract.md` | dev |
| 2026-10-28 | 新增 | 日志查询 | 请求详情补记请求头与响应头（migration 000007）：k6 侧对 Cookie/Authorization/Token/*Key*/Set-Cookie 等敏感头脱敏为 ***，均截断 4KB；前端请求详情展开新增「请求头」「响应头」；「采集响应体」开关语义拓展为「采集请求明细」 | `specs/api-contract.md` §2.6 | dev |
| 2026-10-28 | 修正 | 测试管理 | 前置步骤 Cookie 自动传播：k6 setup 阶段收集响应 Set-Cookie 并注入各 VU cookie jar（解决「登录成功但业务接口报会话过期」——网关会话 = ADMINSESSIONID Cookie + csrfToken 头，k6 cookie jar 不跨 VU 共享） | `prds/PRD-performance-testing-platform.md` §3.3.6.1 | dev |
| 2026-10-28 | 修正 | 全平台表单 | 请求头/请求体统一为 Postman 式行编辑：HeaderRowsEditor（启用/键/值 + 变量高亮与点击插入）、BodyRowsEditor（路径键值行表达嵌套 attrs.ouId / list[0].id + JSON 预览 + 粘贴 JSON 拆解）、RequestImportDialog（原始 HTTP 报文/JSON 一键识别回填方法/地址/头/体） | `specs/api-contract.md` | dev |
| 2026-10-28 | 修正 | 测试管理 | Cookie 传播加固：每迭代写回（避免网关轮换出未认证 session 覆盖 jar）、保留 Path/Domain/Secure/HttpOnly、变量键改 __af_cookies 并禁止用户变量使用 __af_ 前缀、setup 未获得 Cookie 时写测试日志告警；定位并清理导致 401 的接口级 Cookie 认证头残留 | `prds/PRD-performance-testing-platform.md` §3.3.6.1 | dev |
| 2026-10-28 | 修正 | 请求配置 | 请求体改为「键值行 + +嵌套按钮」：容器行（对象/数组）承载子级，不再要求手写 a.b 路径；接口弹窗新增 4 个分区（基本信息/请求头/请求体/超时与描述）；请求头支持粘贴识别填充 | `specs/api-contract.md` | dev |
