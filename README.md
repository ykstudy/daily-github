# Daily GitHub - 每日开源推荐

使用 AI 生成每日 GitHub 开源项目推荐，通过 Web 页面浏览。

## 项目结构

```
daily-github/
├── cmd/
│   └── server/
│       └── main.go    # 服务入口（Web + MCP）
├── internal/       # 内部实现（config、storage、service、mcp 等）
├── go.mod         # Go 模块定义
├── index.html     # Web 浏览页面
├── data/          # 存放每日推荐 MD 文件
│   └── YYYY-MM-DD.md
│   └── recommendation-history.json
├── .env.example   # 环境变量示例
└── README.md
```

## 快速开始

### 1. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env 填入你的 API Key
```

程序启动时会自动读取项目根目录下的 `.env` 文件，并注入到进程环境变量中。
如果同名系统环境变量已经存在，则保持系统环境变量优先。

也可以直接导出：

```bash
export OPENAI_API_KEY="your-api-key"
```

### 2. 运行

```bash
go run ./cmd/server
```

程序会：
1. 检查今天的推荐文件是否已存在（`data/YYYY-MM-DD.md`）
2. 如果不存在，抓取 GitHub Trending 的日榜、周榜、月榜作为主要推荐参考
3. 结合当前通用推荐逻辑与历史去重约束，调用 LLM 生成
4. 将当天推荐过的项目写入 `data/recommendation-history.json`
5. 按配置的定时规则持续生成每日推荐
6. 启动 Web 服务器（默认端口 18080）
7. 当 `MCP_ENABLED=true` 时，启动 MCP 服务（默认端口 18081，默认路径 `/mcp`）

访问 http://localhost:18080/daily-github/ 浏览推荐内容；根路径 `/` 也保留兼容，便于本地直连调试。

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `OPENAI_API_KEY` | 是 | - | API 密钥 |
| `OPENAI_BASE_URL` | 否 | `https://api.openai.com/v1` | API 地址（兼容 DeepSeek 等） |
| `OPENAI_MODEL` | 否 | `gpt-4o` | 使用的模型 |
| `DATA_DIR` | 否 | `data` | Markdown 文件存储目录 |
| `GENERATE_ON_STARTUP` | 否 | `true` | 启动时是否生成当天文件 |
| `GENERATE_CRON` | 否 | `5 0 * * *` | 定时生成的 cron 表达式 |
| `GENERATE_RETRY_COUNT` | 否 | `2` | 单次生成失败后的重试次数 |
| `GENERATE_RETRY_DELAY` | 否 | `30s` | 每次失败后的重试间隔 |
| `GENERATE_TIMEZONE` | 否 | `Local` | 定时生成所用时区 |
| `MANUAL_TRIGGER_TOKEN` | 否 | - | 手动触发生成接口的鉴权 token；未配置则接口关闭 |
| `SERVER_READ_TIMEOUT` | 否 | `15s` | HTTP 读超时 |
| `SERVER_WRITE_TIMEOUT` | 否 | `120s` | HTTP 写超时 |
| `SERVER_IDLE_TIMEOUT` | 否 | `120s` | HTTP 空闲超时 |
| `SERVER_SHUTDOWN_TIMEOUT` | 否 | `15s` | 优雅停机超时 |
| `PORT` | 否 | `18080` | Web 服务端口 |
| `MCP_ENABLED` | 否 | `false` | 是否启用 MCP 服务 |
| `MCP_PORT` | 否 | `18081` | MCP 服务端口 |
| `MCP_PATH` | 否 | `/mcp` | MCP 服务路径 |
| `MCP_BEARER_TOKEN` | 启用 MCP 时是 | - | MCP Bearer Token |

优先级说明：系统环境变量 > `.env` 文件 > 代码默认值。

## 线上部署特性

- **定时生成**：服务启动后会按 `GENERATE_CRON` 自动执行每日推荐生成
- **Trending 维度增强**：每次生成前抓取 GitHub Trending 的日榜、周榜、月榜，作为主要候选来源
- **历史去重**：自动记录每天推荐过的 owner/repo，后续生成时避免重复推荐
- **启动补偿**：`GENERATE_ON_STARTUP=true` 时，服务启动会先补一次当天文件
- **失败重试**：生成失败后按 `GENERATE_RETRY_COUNT` 和 `GENERATE_RETRY_DELAY` 自动重试
- **路径前缀兼容**：Web 页面、API 与健康检查同时支持 `/daily-github/...` 前缀，方便挂到 Nginx 子路径
- **手动触发**：提供受 token 保护的 `/daily-github/api/generate` 接口，可按需补生成指定日期
- **健康检查**：提供 `/daily-github/healthz` 端点，适合容器探针和负载均衡检查
- **优雅停机**：收到 `SIGTERM` / `SIGINT` 后会先停止定时任务，再关闭 HTTP 服务
- **持久化友好**：通过 `DATA_DIR` 控制数据目录，容器场景下建议挂载持久卷

## Docker 部署

### 构建镜像

```bash
docker build -t daily-github:latest .
```

### 启动容器

```bash
docker run -d \
	--name daily-github \
	-p 18080:18080 \
	--env-file .env \
	-v $(pwd)/data:/app/data \
	daily-github:latest
```

镜像启动时会先修正 `DATA_DIR` 的目录权限，然后再以非 root 用户 `app` 运行主程序。
如果使用 bind mount，建议仍然预先创建宿主机 `data` 目录；如果部署平台限制 `chown`，优先改用 Docker named volume。

### 使用 docker compose

```bash
docker compose up -d --build
```

启动后可以通过以下地址检查服务：

- 页面首页：http://localhost:18080/daily-github/
- 健康检查：http://localhost:18080/daily-github/healthz
- MCP readiness：http://localhost:18081/mcp/readyz（启用 MCP 时）

如果部署到线上容器平台，建议至少配置：

- `OPENAI_API_KEY`
- `GENERATE_CRON`
- `GENERATE_TIMEZONE`
- `MANUAL_TRIGGER_TOKEN`
- 数据卷挂载到 `/app/data`

## 手动触发生成

配置 `MANUAL_TRIGGER_TOKEN` 后，可通过接口手动触发补生成。
接口默认不会覆盖已存在文件；如果目标日期文件已经存在，会直接返回 `skipped`。

### 生成当天内容

```bash
curl -X POST \
	-H "X-Trigger-Token: your-secret-token" \
	http://localhost:18080/daily-github/api/generate
```

### 生成指定日期

```bash
curl -X POST \
	-H "Content-Type: application/json" \
	-H "X-Trigger-Token: your-secret-token" \
	-d '{"date":"2026-04-18"}' \
	http://localhost:18080/daily-github/api/generate
```

成功时返回 `generated` 或 `skipped`，失败时返回错误信息和已尝试次数。

## 推荐来源与去重规则

- 每次生成都会抓取 GitHub Trending 的 `daily`、`weekly`、`monthly` 三个维度
- Trending 是主要推荐来源，但不会完全限制推荐范围，仍允许补充少量非 Trending 的优质项目
- 程序会从每日生成结果中提取 `owner/repo` 并写入 `data/recommendation-history.json`
- 如果历史 markdown 已存在但历史文件缺失，程序会自动从已有 markdown 回填历史记录

### 使用 DeepSeek

```bash
export OPENAI_API_KEY="your-deepseek-key"
export OPENAI_BASE_URL="https://api.deepseek.com/v1"
export OPENAI_MODEL="deepseek-chat"
```

### 使用其他 OpenAI 兼容 API

只要支持 `/chat/completions` 接口的服务都可以使用，只需修改 `OPENAI_BASE_URL` 和 `OPENAI_MODEL`。

## 功能特性

- **智能跳过**：已存在的日期文件不会重复生成
- **Markdown 优化渲染**：表格、代码块、引用块等元素均有优化样式
- **响应式设计**：支持桌面和移动端浏览
- **日期搜索**：侧边栏支持日期过滤搜索
- **语法高亮**：代码块支持语法高亮
- **无障碍支持**：键盘导航、跳过链接、ARIA 标签
