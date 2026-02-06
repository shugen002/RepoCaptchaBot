Repo Captcha Bot
================

一个简单的基于仓库内容进行验证的Telegram机器人。

他可以帮助你在Telegram群组中防止机器人和垃圾信息，通过要求新成员解决基于GitHub仓库内容的验证码来验证他们的身份。

本项目使用 Golang 编写，并利用 GitHub API 获取仓库信息，数据使用sqlite存储。

### 验证问题的示例

- 最近一次提交的作者是谁？
- 仓库的主要编程语言是什么？
- 最后一次提交的提交信息是什么？  
- 最后的Release版本号是多少？
- 某文件的第10行内容是什么？

### 功能特点

1. **基于仓库内容的验证码**：每次新成员加入时，机器人会从指定的GitHub仓库中提取信息，生成独特的验证码问题。
2. **自动验证**：新成员必须正确回答验证码问题才能获得群组访问权限。
3. **多种问题类型**：支持多种类型的问题，确保验证码的多样性和安全性。
4. **易于配置**：群组管理员可在群内通过 `/setrepo owner/name` 命令实时配置目标仓库。
5. **开源和可定制**：代码开源，允许用户根据自己的需求进行修改和扩展。

### 系统设计

#### 架构概览

```
┌────────────┐      ┌──────────────┐      ┌──────────────┐
│Telegram API│◄────►│Bot Service  │◄────►│GitHub Client │
└────────────┘      └─────┬────────┘      └──────┬───────┘
                           │                      │
                           ▼                      │
                      ┌────────┐                  │
                      │Verifier│                  │
                      └────┬───┘                  │
                           ▼                      │
                     ┌──────────┐                 │
                     │SQLite DB │◄────────────────┘
                     └──────────┘
```

- **Bot Service**：封装 Telegram Bot API 交互，负责监听 `ChatJoinRequest` 事件。
- **Verifier**：生成验证码问题、验证回答、处理超时逻辑，是业务的核心状态机。
- **GitHub Client**：调用 GitHub API，缓存必要的仓库元信息，确保问题来源多样且可配置。
- **SQLite 存储层**：记录待验证成员、已生成的问题、回答状态和审计日志，保证流程可追踪。

#### 关键流程

1. **入群申请**：Bot 监听 `ChatJoinRequest`（若群未启用申请则回退到 `ChatMemberUpdated`），写入 `pending_members`，并立即与申请者建立私聊会话。
2. **生成问题**：Verifier 基于配置的仓库从缓存/实时 API 中取数据，按照策略选择题型并落库。
3. **发送验证码**：Bot 会尝试在私聊中发送问题，要求用户在120秒内回答；若私聊失败则放弃。
4. **收集回答**：在私聊中监听用户回答，匹配落库问题并验证正确性。
5. **授予权限**：回答正确则调用 `approveChatJoinRequest` ，失败/超时则拒绝申请，可选地重新发送问题。
6. **审计与清理**：定期任务回收过期问题、统计通过率并推送给管理员。

> 注意：Telegram 仅允许已与机器人对话的用户接收私聊消息。Bot 在捕获 `ChatJoinRequest` 时应先 `sendMessage` 提示用户点击 “Start” 再发送正式题目。

#### 数据模型（建议）

| 表名 | 关键字段 | 说明 |
| --- | --- | --- |
| `pending_members` | `telegram_id`, `chat_id`, `question_id`, `expires_at` | 待验证成员状态 |
| `questions` | `id`, `repo`, `type`, `payload`, `answer` | 生成的验证码题与答案 |
| `audit_logs` | `id`, `action`, `actor`, `detail`, `created_at` | 管理员操作和系统事件 |

#### 配置与扩展点

- **环境变量**：`BOT_TOKEN`, `GITHUB_TOKEN`, `DB_PATH` 等。
- **题型插件化**：按接口 `QuestionProvider` 注册新题型，方便扩展如 CI 状态、Issue 统计等问题。
- **缓存策略**：使用内存缓存或 Redis（可选）降低 GitHub API 调用频率，失效策略可设为 $5\text{min}$ 或基于 `ETag`。
- **安全策略**：
  - 限制单用户回答频率，防止暴力破解。
  - 使用 Telegram `restrictChatMember` 降低可见性，避免问题泄露。
  - 对日志与配置进行脱敏，避免泄漏 token。

#### 运行与观察性

- **指标**：成功率、踢出率、平均响应时间、GitHub API 调用次数。
- **日志**：结构化输出 JSON，字段包括 `chat_id`, `member_id`, `question_type`, `result` 等，便于接入 Loki/ELK。
- **告警**：当 Github API 降级、回答正确率异常、数据库不可用时发送管理员通知。

### 部署

### 配置

环境变量示例：

- `BOT_TOKEN`：Telegram Bot Token（必填）
- `DB_PATH`：SQLite 数据库路径（可选，默认 `./repo_captcha_bot.db`）
- `GITHUB_TOKEN`：GitHub Token（可选，避免 API 限速）
- `QUESTION_TTL`：答题超时时间（可选，默认 120s，如 `2m`）
- `FILE_PATH`：用于“文件第 N 行”题型的文件路径（可选，作为新建群配置的默认值）
- `FILE_LINE`：用于“文件第 N 行”题型的行号（可选，作为新建群配置的默认值）

#### 群组内配置

机器人启动后需要在目标群组中执行一次 `/setrepo owner/name` 命令（仅管理员可用），仓库信息会写入数据库并立即生效。每个群可以配置不同的仓库，若后续需要修改可重复执行该命令。

#### Docker Compose 
```yaml
version: '3.8'
services:
  repo-captcha-bot:
    image: shugen002/repo-captcha-bot:latest
    container_name: repo-captcha-bot
    restart: unless-stopped
    environment:
      - BOT_TOKEN=your_telegram_bot_token
      - DB_PATH=/data/repo_captcha_bot.db
      - GITHUB_TOKEN=your_github_token # 可选，但推荐使用以避免API速率限制
    volumes:
      - ./data:/data
```

### 感谢

感谢 Golang 社区和所有开源贡献者，使得这个项目成为可能！