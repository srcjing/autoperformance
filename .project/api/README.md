# API 可视化（开发期工具）

本目录提供接口的可视化列表与示例，**仅用于开发期**，不随离线部署包分发。

## 文件

| 文件 | 作用 |
| --- | --- |
| `openapi.json` | **接口定义的单一数据源**（OpenAPI 3.0），所有接口、参数、响应、权限点、错误码的唯一权威定义 |
| `index.html` | 自包含可视化页面，读取 `openapi.json` 自动渲染可搜索、可展开、带请求/响应示例的接口列表 |

## 使用方式

在 `.project/api` 目录下启动本地静态服务（二选一）：

```bash
python -m http.server 8000
# 或
npx serve .
```

浏览器访问：<http://localhost:8000/index.html>

> 页面通过 `fetch('openapi.json')` 加载数据，因此需走 HTTP 服务，直接双击 `file://` 无法读取（页面会提示）。

## 新增接口（自动加入列表）

**只需改 `openapi.json` 一处**，刷新页面即自动出现在列表中：

1. 在 `paths` 下新增路径与方法（如 `"/api/v1/xxx": { "post": { ... } }`）；
2. 补全 `summary`、`parameters`、`requestBody`、`responses`；
3. 标注扩展字段 `x-permission`（权限点，如 `env.list:write`）与 `x-err-codes`（错误码数组）；
4. 若新增请求/响应结构，在 `components.schemas` 下补充对应 schema（用 `$ref` 引用）。

页面会自动按模块分组、生成请求/响应示例（示例由 schema 字段类型自动生成，无需手写）。

## 约定

- **单一数据源**：接口定义只维护 `openapi.json`，可视化页面与未来 SDK/校验工具均从此派生，不手工维护第二份接口清单。
- **与错误码联动**：每个 operation 的 `x-err-codes` 与 `.project/specs/errcode.md` 的错误码清单保持一致。
- **不随包分发**：本目录属于设计/开发辅助材料，构建离线部署包时排除（见部署打包白名单）。
