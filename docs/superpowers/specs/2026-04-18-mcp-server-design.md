# Daily GitHub MCP Server Design

## Goal

在当前 Daily GitHub 单体 Go 服务中新增一个对外提供 MCP 能力的远程 HTTP server，满足以下约束：

- 使用官方 Go MCP SDK。
- 保持单进程部署，不新增独立镜像。
- 首期仅暴露现有业务能力，不额外引入 AI agent 型工具。
- AI 相关参数在服务启动期固定配置，不允许请求级覆盖。
- 在 AI 配置与连通性验证通过前，MCP 客户端不能建立可用连接。
- 具备基础工程性，包括模块边界、配置管理、鉴权、日志、测试与验证流程。

## Non-Goals

- 首期不提供 MCP Resources。
- 首期不提供 MCP Prompts。
- 首期不实现 OAuth 或企业级授权服务器。
- 首期不拆分为独立 MCP 进程或独立仓库。
- 首期不重做现有页面与 REST API 设计。

## Existing Context

当前项目的主要特征：

- 单入口服务，核心逻辑集中在 [main.go](main.go)。
- 已有 Web 页面和 REST API，提供日期列表、内容读取、手动触发生成与健康检查。
- 已有 AI 配置：OPENAI_API_KEY、OPENAI_BASE_URL、OPENAI_MODEL。
- 已有容器化部署与数据持久化约定。
- 生成逻辑、文件存取、HTTP handler 目前耦合较高。

这意味着新增 MCP 时，重点不是再造一套业务逻辑，而是把现有能力提炼成可复用服务层，并在其上挂接官方 MCP 协议层。

## Recommended Library

推荐使用官方 SDK：github.com/modelcontextprotocol/go-sdk。

选择理由：

- 官方维护，协议演进同步更稳。
- 已提供远程 HTTP 的 Streamable HTTP transport。
- 提供标准的 `mcp.NewServer`、`mcp.AddTool`、`mcp.NewStreamableHTTPHandler` 组合。
- 标准 HTTP middleware 可直接包裹 MCP handler，适合接入 Bearer Token 鉴权与 readiness 门禁。

首期服务端传输方案使用 Streamable HTTP，不使用 stdio。

## High-Level Architecture

采用单进程双服务架构：

1. 现有 Web server 继续负责页面与 REST API。
2. 新增 MCP server 使用独立端口提供远程 MCP 访问。

单进程保留现有部署简单性，独立端口保证 Web 与 MCP 在超时、日志、鉴权和就绪控制上相互隔离。

### Proposed Internal Modules

#### cmd/server

总启动入口。负责：

- 加载配置
- 初始化存储、业务服务、readiness 检查器
- 启动 Web server
- 启动 MCP server
- 统一优雅停机

#### internal/config

负责环境变量解析、默认值填充和配置校验。

配置分为：

- 现有 Web 配置
- 现有生成与 AI 配置
- 新增 MCP 配置

#### internal/storage

封装与 `data` 目录相关的读写：

- 枚举可用日期
- 读取某日 Markdown
- 读取与写入推荐历史
- 文件存在性检查

#### internal/service

封装业务能力，作为 REST 和 MCP 的共同复用层。

首期至少包含：

- DateService: 列出日期
- ContentService: 读取某日内容
- GenerationService: 触发指定日期或当天生成

#### internal/readiness

封装 MCP 可用性门禁状态与 AI 配置验证流程。

职责：

- 启动后执行静态配置校验
- 执行 AI 连通性探测
- 维护线程安全的 readiness 状态
- 对外暴露只读查询接口

#### internal/mcpserver

负责 MCP 协议层适配：

- 创建官方 MCP server
- 注册 tools
- 构造 streamable HTTP handler
- 挂接认证中间件
- 挂接 readiness 门禁中间件
- 记录访问日志与错误日志

## Runtime Flow

### Startup Flow

1. 加载 `.env` 与系统环境变量。
2. 解析已有配置与新增 MCP 配置。
3. 初始化存储和业务服务。
4. 启动 readiness 检查器。
5. 启动 Web server。
6. 启动 MCP server。
7. readiness 通过前，MCP 路径始终返回 503。
8. readiness 通过后，MCP handler 开始接受标准 MCP 会话初始化与 tool 调用。

### Request Flow

对 MCP 请求，按如下顺序处理：

1. HTTP Bearer Token 鉴权。
2. readiness 门禁检查。
3. MCP Streamable HTTP handler 建立或复用会话。
4. tool handler 调用内部 service。
5. service 调用 storage 或复用现有生成逻辑。
6. 返回结构化结果。

## MCP Surface

首期只暴露 tools，不暴露 resources 和 prompts。

### Tool 1: list_dates

用途：返回当前已生成内容的日期列表。

输入：

- `prefix`，可选。按日期前缀过滤，例如 `2026-04`。

输出：

- `dates`: 日期字符串数组，按新到旧排序。
- `count`: 返回数量。
- `latest`: 最近日期，没有数据时为空字符串。

错误处理：

- 存储读取失败时返回 tool error。

### Tool 2: get_daily_content

用途：读取指定日期的 Markdown 内容。

输入：

- `date`，必填，格式 `YYYY-MM-DD`。

输出：

- `date`: 请求日期。
- `exists`: 是否存在。
- `content`: Markdown 原文。不存在时为空。
- `content_type`: 固定为 `text/markdown`。

错误处理：

- 日期格式非法返回参数错误。
- 文件读取失败返回 tool error。
- 文件不存在时返回 `exists=false`，不视为系统错误。

### Tool 3: generate_daily_content

用途：触发当天或指定日期的内容生成。

输入：

- `date`，可选。为空时默认使用服务当前时区下的当天。

输出：

- `date`: 实际执行日期。
- `status`: `generated`、`skipped` 或 `failed`。
- `generated`: 布尔值。
- `attempts`: 实际尝试次数。
- `message`: 结果说明。

错误处理：

- 日期格式非法返回参数错误。
- AI 上游不可用、GitHub 抓取失败、持久化失败返回 tool error。
- 目标文件已存在时，返回 `status=skipped`，不视为系统错误。

## AI Configuration Strategy

MCP 不引入单独的 AI 配置源，直接复用当前服务已有参数：

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `OPENAI_MODEL`

理由：

- 避免 Web/REST/MCP 三条入口使用不同模型配置。
- 降低运维复杂度。
- 便于把 readiness 设计成单一事实来源。

MCP 首期不允许通过 tool 参数覆盖模型、base URL 或 API key。

## New Configuration

新增以下环境变量：

- `MCP_ENABLED`
  - 默认值：`false`
  - 是否启用 MCP server。

- `MCP_PORT`
  - 默认值：`18081`
  - MCP 服务监听端口。

- `MCP_PATH`
  - 默认值：`/mcp`
  - MCP Streamable HTTP 路径。

- `MCP_BEARER_TOKEN`
  - 默认值：空
  - 远程连接 MCP 必需的 Bearer Token。

- `MCP_READY_TIMEOUT`
  - 默认值：`15s`
  - AI 连通性验证的单次超时。

- `MCP_READY_RETRY_COUNT`
  - 默认值：`3`
  - readiness 动态探测失败时的重试次数。

- `MCP_READY_RETRY_DELAY`
  - 默认值：`10s`
  - readiness 动态探测失败后的重试间隔。

- `MCP_SERVER_READ_TIMEOUT`
  - 默认值：`15s`

- `MCP_SERVER_WRITE_TIMEOUT`
  - 默认值：`300s`

- `MCP_SERVER_IDLE_TIMEOUT`
  - 默认值：`120s`

## Readiness Design

### Desired Behavior

MCP server 可以监听端口，但在验证通过前不能建立可用连接。

因此，MCP readiness 的行为定义为：

- Web 服务启动不依赖 MCP readiness 成功。
- MCP 服务是否监听，取决于 `MCP_ENABLED`。
- `MCP_ENABLED=true` 时，MCP handler 在 readiness 未通过前统一返回 HTTP 503。
- readiness 通过后，MCP handler 才允许进入 MCP 协议处理。

### Readiness States

建议显式维护以下状态：

- `disabled`: 未启用 MCP。
- `pending`: readiness 尚未完成。
- `ready`: 配置与连通性验证通过。
- `failed`: readiness 已结束且未通过。

### Validation Steps

#### Static Validation

校验以下条件：

- `OPENAI_API_KEY` 非空。
- `OPENAI_BASE_URL` 可解析且为合法 URL。
- `OPENAI_MODEL` 非空。
- `MCP_BEARER_TOKEN` 非空。
- 超时与重试配置合法。

#### Dynamic Validation

执行最小化的 AI 探活请求，不生成日报，只验证连通性和模型可用性。

建议探测请求特征：

- 使用当前配置的 `/chat/completions` 接口。
- 输入单条极短消息，例如 `reply with ok`。
- `max_tokens` 设为极小值。
- 请求超时采用 `MCP_READY_TIMEOUT`。

判定标准：

- 上游返回成功响应，即 readiness 成功。
- 上游 401/403/404/429/5xx 或超时均视为 readiness 未通过。

### Readiness Exposure

新增一个只读状态端点，用于运维和验证：

- `GET /mcp/readyz`

返回字段建议：

- `enabled`
- `state`
- `lastError`
- `checkedAt`

该端点不走 MCP 协议，只作为 HTTP 诊断接口。

## Authentication

首期认证采用静态 Bearer Token，不引入 OAuth。

请求要求：

- `Authorization: Bearer <token>`

认证策略：

- 缺失或错误 token 时返回 HTTP 401。
- 比较逻辑使用常量时间比较，减少时序信息泄漏。

理由：

- 当前仓库已有类似 token 控制经验。
- 满足对外开放前的基础接入控制。
- 工程复杂度明显低于 OAuth。

## Security Considerations

- MCP 与 Web 使用独立端口，降低路由混用与超时干扰。
- Bearer Token 与现有 `MANUAL_TRIGGER_TOKEN` 分离，避免权限混用。
- 使用官方 Streamable HTTP handler 默认的跨域与 localhost 保护能力，不主动关闭。
- 生产环境建议仅通过反向代理或内网暴露 MCP 端口。
- 日志中不记录 Bearer Token、API key 与完整上游响应体。

## Logging and Observability

新增 MCP 相关日志建议包含：

- server 启动与停止
- readiness 状态变更
- readiness 探活失败摘要
- MCP 认证失败
- tool 调用名称、结果状态、耗时

不记录：

- 敏感头
- 完整 Markdown 内容
- 完整 LLM 请求体

## Error Handling

建议错误分层：

### HTTP Layer

- 401: Bearer Token 不合法。
- 503: readiness 未通过。
- 500: MCP handler 初始化或内部未预期错误。

### MCP Tool Layer

- 参数错误：返回明确的 validation error。
- 业务可预期状态：使用正常结果结构表达，例如 `exists=false` 或 `status=skipped`。
- 系统错误：返回 tool error，并保留可观测日志。

## File Layout Changes

建议目标目录结构如下：

```text
cmd/
  server/
    main.go
internal/
  config/
    config.go
  storage/
    markdown_store.go
    history_store.go
  service/
    dates.go
    content.go
    generation.go
  readiness/
    checker.go
  mcpserver/
    server.go
    middleware.go
    tools.go
```

说明：

- 允许分阶段迁移，不要求一次性把现有逻辑全部拆完。
- 初始版本可先把旧 `main.go` 中的逻辑迁入新包，再逐步缩减入口文件。

## Testing Strategy

### Unit Tests

至少覆盖：

- MCP 配置解析与默认值
- Bearer Token 鉴权
- readiness 状态流转
- tool 参数校验
- tool 输出结构化映射

### Integration Tests

使用官方 `mcp.StreamableClientTransport` 连接本地 `httptest` MCP server，覆盖：

1. readiness 未通过时，访问 MCP 返回 503。
2. readiness 通过后，客户端可以成功 initialize。
3. `list_dates` 返回日期列表。
4. `get_daily_content` 对存在日期返回 Markdown。
5. `get_daily_content` 对不存在日期返回 `exists=false`。
6. `generate_daily_content` 对已存在日期返回 `skipped`。

### Manual Verification

实现完成后，至少执行以下人工验证：

1. 配置合法且上游可达时，`/mcp/readyz` 变为 `ready`。
2. 使用错误 token 请求 MCP，返回 401。
3. 使用正确 token，客户端能够列出 tools。
4. 成功调用 `list_dates` 与 `get_daily_content`。
5. `generate_daily_content` 能返回 `generated` 或 `skipped`。

## Rollout Plan

### Phase 1

- 引入官方 Go MCP SDK。
- 新增配置解析。
- 建立最小 service 层。
- 启动独立 MCP server。
- 注册 3 个 tools。
- 完成 readiness 与 token 门禁。

### Phase 2

- 增加更细粒度日志和指标。
- 根据客户端需求决定是否补充 resources 或 prompts。

## Trade-Offs

### Why Not Mount MCP Into Existing Web Mux

不推荐直接塞进现有 mux，原因：

- MCP 与 Web 的超时需求不同。
- 鉴权策略不同。
- readiness 门禁语义不同。
- 后续演进 resources、prompts 或不同 transport 时扩展受限。

### Why Not Separate Process

当前阶段不拆独立进程，原因：

- 当前仓库尚未形成清晰内部 API 边界。
- 独立进程会引入额外部署、配置和运维复杂度。
- 现阶段收益不如单进程双服务明确。

## Acceptance Criteria

满足以下条件时，视为本设计完成：

- 项目引入官方 Go MCP SDK。
- MCP server 通过独立 HTTP 端口对外提供 Streamable HTTP transport。
- 首期只提供 `list_dates`、`get_daily_content`、`generate_daily_content` 三个 tools。
- MCP 使用独立 Bearer Token 鉴权。
- AI 配置在启动期固定，不允许请求级覆盖。
- readiness 未通过前，MCP 端点返回 503，客户端不可用。
- 提供 `GET /mcp/readyz` 状态查看。
- 具备最小单元测试与集成测试。
