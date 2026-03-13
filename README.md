# OpenAPI2Word

使用 Go + Gin 解析 Swagger/OpenAPI（v2/v3），并通过 WordZero 生成排版好的 Word（.docx）。

## 快速开始

1. 安装依赖：

```
go mod tidy
```

2. 启动服务：

```
go run ./cmd/server
```

## 接口

### POST /api/parse

- 入参：
  - `multipart/form-data`，字段 `file`（JSON/YAML）
  - 或 JSON：`{ "url": "https://.../openapi.json" }`
- 出参：`{ "doc": APIDocument }`

### POST /api/generate

- 入参：`{ "doc": APIDocument, "meta": Meta, "endpoint_ids": [] }`
  - `endpoint_ids` 为空则导出全部接口
- 出参：`.docx` 二进制流

## 文档生成说明

- 标题与二级标题为黑色。
- 接口标题从 `1.xxx` 递增编号。
- “接口说明/接口设计”采用三级标题。
- 接口详情表格为 4 列结构，包含：
  - 接口地址、请求方式、请求参数、响应参数
  - 请求/响应参数说明采用表头行（字段名称/字段类型/是否必传/备注）
- 响应参数展示顺序：先展示父级字段，再展示子集说明段。
  - 例如 `data` 先列在顶层
  - 子集说明按“xxx说明 + 表头 + 子集字段”展开

## 目录结构

- `cmd/server`：服务启动入口
- `internal/api`：HTTP 接口
- `internal/parser`：Swagger/OpenAPI 解析
- `internal/docgen`：Word 生成逻辑
- `internal/model`：数据模型
