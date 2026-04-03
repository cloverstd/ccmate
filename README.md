# ccmate

将 Coding Agent（Claude Code、Codex、Gemini）与 GitHub 项目管理集成的平台。监听 Issue/PR 事件，自动触发 Agent 任务，生成代码并创建 PR。

## 依赖

- Go 1.21+
- [bun](https://bun.sh/)
- SQLite3

## 配置

复制示例配置并按需修改：

```bash
cp config.example.yaml config.yaml
```

配置项通过环境变量覆盖，前缀 `CCMATE_`，层级用 `_` 分隔，如 `CCMATE_SERVER_PORT=9090`。

## 开发

### 后端

```bash
# 安装依赖
go mod tidy

# 生成 ent 代码（修改 schema 后需要重新执行）
go generate ./...

# 启动开发服务（默认 :8080）
go run ./cmd/ccmate-server -config config.yaml

# 运行测试
go test ./... -v

# Lint（需安装 golangci-lint）
golangci-lint run ./...
```

### 前端

```bash
# 安装依赖
cd web && bun install

# 启动开发服务（自动代理 /api 到后端 :8080）
cd web && bun run dev

# 构建（输出到 internal/static/dist/，嵌入 Go 二进制）
cd web && bun run build
```

## 构建

```bash
# 先构建前端
cd web && bun run build && cd ..

# 再构建后端（含嵌入的前端资源）
go build -o bin/ccmate-server ./cmd/ccmate-server
```

## 运行

```bash
./bin/ccmate-server -config config.yaml
```

首次启动会在日志中输出 bootstrap token，用于注册管理员 Passkey。

## Prompt Templates

Prompt Templates 用于自定义发送给 Agent 的 task prompt。模板使用 Go template 语法，支持以下变量：

| 变量 | 类型 | 说明 |
|------|------|------|
| `{{.IssueNumber}}` | int | Issue 编号 |
| `{{.IssueTitle}}` | string | Issue 标题 |
| `{{.IssueBody}}` | string | Issue 正文 |
| `{{.IssueLabels}}` | []string | Issue 标签列表 |
| `{{.IssueUser}}` | string | Issue 创建者 |
| `{{.IssueLink}}` | string | Issue 链接（如 https://github.com/owner/repo/issues/1） |
| `{{.RepoOwner}}` | string | 仓库 Owner |
| `{{.RepoName}}` | string | 仓库名称 |
| `{{.RepoFullName}}` | string | 完整仓库名（owner/name） |
| `{{.TaskType}}` | string | 任务类型（issue_implementation / review_fix / manual_followup） |
| `{{.BranchName}}` | string | 工作分支名 |

模板还提供以下辅助函数：

| 函数 | 说明 | 示例 |
|------|------|------|
| `has` | 检查字符串切片是否包含某项 | `{{if has .IssueLabels "bug"}}...{{end}}` |
| `join` | 用分隔符连接字符串切片 | `{{join .IssueLabels ", "}}` |

### 示例

```
请使用 Go 编写，遵循项目现有代码风格。
{{if has .IssueLabels "bug"}}这是一个 Bug 修复任务，请确保添加回归测试。{{end}}
{{if eq .TaskType "review_fix"}}请仔细阅读 review 反馈并逐条修复。{{end}}
```

### Scope 策略

每个 Project 可以设置 Template Scope：

- **Project Only**：只使用项目自己的模板
- **Global Only**：只使用全局默认模板（在 Settings 中配置）
- **Merged**：全局模板 + 项目模板合并（全局在前，项目在后）

## 项目结构

```
cmd/ccmate-server/         服务入口
internal/
  config/                  配置加载
  ent/schema/              数据模型（13 个 ent schema）
  api/                     HTTP 路由、handler、中间件
  auth/                    Passkey 认证
  sse/                     SSE 事件广播
  scheduler/               任务调度、状态机、并发控制
  runner/                  Agent 子进程执行、工作目录管理
  gitprovider/             Git 平台抽象 + GitHub 实现
  agentprovider/           Agent 适配抽象 + Claude Code / Mock 实现
  webhook/                 Webhook 验签、去重、命令解析
  prompt/                  Prompt 分层组装、UNTRUSTED_CONTEXT 反注入
  sanitize/                日志脱敏、XSS 防护
  audit/                   审计日志
  model/                   共享领域类型
  static/                  前端嵌入
web/                       React 前端（Vite + shadcn/ui）
config.example.yaml        配置示例
```
