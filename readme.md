RepoCaptchaBot
==============

基于 GitHub 仓库内容的人机验证机器人，帮助 Telegram 群管理者拒绝批量脚本号。机器人会给每一位正在申请入群的用户发送一题“仓库知识问答”，只有在时限内答对的人才能获批。

## 关键能力

- 自动监听 `ChatJoinRequest` 和回退的 `ChatMemberUpdated` 事件，确保即便群未开启“加入申请”也能触发验证。
- 结合 GitHub API 与本地缓存生成验证码，当前实现了 5 类问题：最近提交作者 / 最近提交信息 / 仓库主语言 / 最新 Release 版本 / 指定文件的第 N 行。
- 题目、答案和待验证成员状态持久化在 SQLite，宕机重启后可继续流程，并附带审计日志。
- 支持按群配置仓库，管理员在群里执行 `/setrepo owner/name` 即可生效，也会继承默认的文件题配置。
- 内置 1 分钟一次的过期清理，自动丢弃失效验证并写入审计。日志统一使用 `log/slog` 的 JSON 输出，方便采集。

## 架构概览

```
Telegram ⇄ BotHandler ⇄ Verifier ⇄ GitHubClient
                        │
                        └── Store (SQLite)
```

- `main.go` 负责加载配置、初始化 SQLite（`modernc.org/sqlite` 驱动）、创建 Telegram bot 并启动循环。
- `BotHandler` 统一处理 Telegram 更新、生成题目、在私聊中校验答案并调用 `ApproveChatJoinRequest`/`DeclineChatJoinRequest`。
- `Verifier` 组合多个问题生成器，挑选第一个可用的题目并返回。
- `GitHubClient` 自带 5 分钟内存缓存，减少对公开 API 的重复请求，必要时可以携带 Personal Access Token。
- `Store` 定义数据表结构及 CRUD，涵盖题目、待验证成员、群配置和审计日志。

## 快速开始

1. 创建 Telegram Bot Token，邀请机器人进群并授予管理员，确保拥有“邀请成员”和“封禁成员”权限。
2. 在群设置中开启“需要管理员批准”或“加入申请”，否则无法收到 `ChatJoinRequest`。
3. 准备环境变量（最少 `BOT_TOKEN`），然后执行：

```bash
go run ./...
```

4. 机器人启动后，在目标群发送 `/setrepo owner/name`（只有管理员可用）。命令成功后，新成员即会收到基于该仓库生成的题目。

SQLite 数据文件默认写在 `./repo_captcha_bot.db`，可通过 `DB_PATH` 指定绝对路径。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `BOT_TOKEN` | (无) | Telegram Bot Token，必填 |
| `GITHUB_TOKEN` | (空) | 可选，减少 GitHub API 限速 |
| `DB_PATH` | `./repo_captcha_bot.db` | SQLite 文件路径 |
| `QUESTION_TTL` | `120s` | 单次验证时限，`time.ParseDuration` 语法 |
| `FILE_PATH` | (空) | 默认文件题的文件路径，未配置群时作为初始值 |
| `FILE_LINE` | `0` | 默认文件题的行号，>0 时才会尝试生成该题型 |
| `BOT_LANG` | `zh-CN` | 机器人语言，决定加载 `i18n/<lang>.ini` |

## 群组使用流程

1. 管理员把机器人拉入群并升为管理员；机器人会自检权限和群设置，缺少权限时会提示。
2. 第一次运行时若未配置仓库，机器人会每 5 分钟提醒一次执行 `/setrepo owner/name`。
3. 有用户申请入群时：
   - `BotHandler` 从 `Store` 读取群配置并调用 `Verifier` 生成题目。
   - 题目（`questions` 表）与待验证信息（`pending_members` 表）会立即落库。
   - 机器人给用户发送私聊题目，要求在 `QUESTION_TTL` 内回答，用户也可使用 `/start` 再次获取题目。
4. 回答正确则批准申请，错误或超时则拒绝并可选踢出；结论会写入 `audit_logs`。

## 题目类型

| 类型 | Prompt | 数据来源 |
| --- | --- | --- |
| `latest_commit_author` | 最近一次提交的作者是谁？ | `GET /repos/{repo}/commits?per_page=1` |
| `latest_commit_message` | 最后一次提交的提交信息是什么？ | 同上 |
| `repo_language` | 仓库的主要编程语言是什么？ | `GET /repos/{repo}` |
| `latest_release` | 最后的 Release 版本号是多少？ | `GET /repos/{repo}/releases/latest`（若不存在则跳过） |
| `file_line` | 文件 `<path>` 的第 `<line>` 行内容是什么？ | `GET /repos/{repo}/contents/{path}` 并解码 Base64 |

所有题目在入库时会记录 `prompt`, `payload`, `answer`，方便后续审计与重发。

## 数据存储

`Store.Init()` 会创建以下表：

- `questions`：记录生成过的题目及答案。
- `pending_members`：以 `(telegram_id, chat_id)` 为主键保存当前待验证用户，含过期时间。
- `chat_configs`：群配置（仓库、文件路径/行号）。
- `audit_logs`：所有自动化动作（发送失败、通过、拒绝、管理员命令）。

后台协程每分钟调用 `CleanupExpired`，删除 `pending_members` 中已过期的记录，防止堆积。

## 开发提示

- 依赖声明在 `go.mod`，当前要求 Go `1.25.7` 或更高。
- 运行 `go run ./...` 即可本地启动；如需交叉编译可直接 `go build ./...`。
- 若部署在受限网络，可通过 `HTTP_PROXY`/`HTTPS_PROXY` 等环境变量让机器人复用宿主代理；`main.go` 已继承 `http.ProxyFromEnvironment`。
- 建议为机器人和 GitHub Token 使用独立的 `.env`/secret 管理，避免写死在代码中。

欢迎基于 `Verifier` 新增题型或扩展更多存储后端，提交 PR 之前可自查日志确保每个分支都覆盖到。

## i18n

每种语言对应一个 `ini` 文件，放在 `i18n/` 目录下，例如：

- `i18n/zh-CN.ini`
- `i18n/en.ini`

通过 `BOT_LANG` 指定语言标识；若缺省或文件不存在，会回退到 `zh-CN`。