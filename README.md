# acn

**acn (Agent Completion Notification) — Agent 任务完成通知**
Claude Code / Codex 跑完一轮，推送到飞书或 Bark。

单个 Go 二进制，零依赖，无界面，无常驻进程。

## 原理

两个 CLI 都原生支持 `Stop` 生命周期 hook，且**载荷 schema 几乎一致**，acn 只是接上它们：

```
Claude Code ──Stop hook──┐                 ┌─→ 飞书
                         ├─→ acn hook ─────┤
Codex ───────Stop hook───┘                 └─→ Bark
```

hook 是个短命进程：解析载荷、并发请求已配置渠道、退出。通常在百毫秒量级，
最慢不超过统一的 3 秒超时。没有 daemon，没有 socket，没有要管的服务。

## 安装

```bash
brew install windyskr/tap/acn
# 或：go install github.com/windyskr/agent-completion-notification/cmd/acn@latest

# 至少配置一个通知渠道，也可以两个都配
acn config feishu-url https://open.feishu.cn/open-apis/bot/v2/hook/xxxx
acn config bark-url https://api.day.app/your_key
acn install
acn doctor                          # 自检 + 实际推送一条
```

`acn install` 会改写这两个文件（**改写前自动备份为 `*.acn.bak`**）：

| 文件 | 改动 |
| --- | --- |
| `~/.claude/settings.json` | 增加一个 `Stop` hook |
| `~/.codex/config.toml` | 追加一个 `[[hooks.Stop]]` 块（哨兵注释界定） |

之后还需两步：

1. Claude Code 与 Codex **重启**后生效；
2. 在 Codex 里执行 `/hooks`，**信任** acn 的 Stop hook——Codex 要求逐条审阅非托管的命令 hook，不信任就不会执行。

## 为什么 Codex 用 hooks 而不是 notify

Codex 的顶层 `notify` 是遗留路径（二进制里那个文件就叫 `legacy_notify.rs`），且**全局只有一个槽位**。抢占它意味着：

- 破坏用户已有的集成（如 Codex Computer Use 就占着这个槽位），除非再实现一套「存下来 → 每次转发 → 卸载还原」的机制；
- 即便实现了，对方下次安装仍会把 acn 静默顶掉，且没有任何报错。

`[[hooks.Stop]]` 可以多个并存，互不干扰，acn 完全不用碰 `notify`。
额外好处：hooks 的载荷带 `transcript_path`，Codex 的耗时也能算出来（取 rollout 里最后一条 `task_started`）——notify 的载荷没有起始时间，那条路做不到。

**要求 Codex ≥0.129**（`hooks` 是默认开启的特性）。acn 只面向新版本，不为旧版做兼容降级。

若你在 `config.toml` 里写过 `[features] hooks = false`，块虽然能写进去但永远不会触发——
`acn status` 与 `acn doctor` 会就此发出警告。

## 命令

```
acn install [claude|codex]     接入 AI CLI，省略目标则两个都装
acn uninstall [claude|codex]   摘除接入
acn status                     查看接入状态与配置
acn doctor                     自检整条链路，并实际推送一条通知
acn config <k> <v>             修改配置
```

### acn doctor

装好之后跑一次，它会主动核对而不只是回显配置：

```
✓ 二进制         /opt/homebrew/bin/acn
✓ Claude Code    hook 已安装 → /opt/homebrew/bin/acn
✓ Codex          hook 已安装 → /opt/homebrew/bin/acn
? Codex 信任     无法自动确认（Codex 未公开该状态）
    → 若 Codex 侧收不到通知，在 Codex 里执行 /hooks 信任 acn
✓ 飞书 webhook   https://open.feishu.cn/…/xxxx…xxxx
? Bark endpoint  未配置（可选）
✓ 实际推送       已发出，请确认已配置渠道是否收到
```

其中最要紧的是**路径核对**：hook 里写的是绝对路径，二进制换了位置（重装到别处、
删掉软链）之后配置仍指向旧路径，两个 CLI 会静默地什么都不做。doctor 会报 `✗` 并
提示重跑 `acn install`。

有检查未通过时退出码为 1，可用于脚本。

Codex 的 hook 信任状态按哈希存在其内部，没有可靠的外部读取方式，
因此这一项永远是 `?`——不会假装检查过。

## 配置

配置在 `~/.acn/config.json`（权限 0600），改完即时生效。

```
acn config feishu-url <url|off> 配置并启用飞书，off 仅关闭渠道
acn config feishu-secret <str|off> 签名密钥，off 清除配置
acn config bark-url <url|off>   配置并启用 Bark，off 仅关闭渠道
acn config feishu <on|off>      是否启用飞书渠道
acn config bark <on|off>        是否启用 Bark 渠道
acn config min-duration <秒>    低于该耗时不推送，0 为不限
acn config device-name <名称|auto>  设置并显示设备名（auto 使用系统 hostname）
acn config show-device-name <on|off>   是否显示设备名，默认 off
acn config show-agent-name <on|off>    是否显示 Agent 名，默认 off
acn config show-project-name <on|off>  是否显示项目名，默认 off
acn config claude-agent-name <名称|auto>  Claude Agent 名，默认 claude
acn config codex-agent-name <名称|auto>   Codex Agent 名，默认 Codex
acn config claude <on|off>      是否推送 Claude Code
acn config codex <on|off>       是否推送 Codex
```

通知标题默认直接使用会话名，例如 `完善组件消融实验方案`。Claude Code 从 transcript
中的 `ai-title` 记录读取；Codex 用 hook 的 `session_id` 查询
`$CODEX_HOME/session_index.jsonl`（默认 `~/.codex/session_index.jsonl`）。取不到会话名时
使用已开启的设备、Agent、项目名前缀；所有前缀均关闭时显示 `任务完成`，避免旧版本
或临时会话产生空标题。

设备名、Agent 名和项目名默认均不参与标题，但原有配置仍然保留。显式开启后会作为
会话名前缀，例如 `MacBookPro-Codex-acn-完善组件消融实验方案`。两个 Agent 名均可
独立配置，传入 `auto` 可恢复各自的默认名称；设置 Agent 名会自动打开 Agent 名显示。

Bark URL 使用 App 复制出的设备端点并截到 key，例如 `https://api.day.app/your_key`；
不要在其后附加 title 或 body。已配置且启用的渠道会并发发送，一个渠道失败不会阻止
另一个渠道尝试，错误信息会注明失败渠道。

设置 `device-name` 会自动打开设备名显示；设置渠道 URL 会自动打开对应渠道。需要覆盖
自动行为时，再显式执行 `show-device-name off`、`feishu off` 或 `bark off`。

环境变量 `ACN_FEISHU_WEBHOOK_URL`、`ACN_FEISHU_SECRET`、`ACN_BARK_URL`、`ACN_DEVICE_NAME`
优先级高于配置文件。
`ACN_CONFIG_DIR` 可改配置目录。

### 耗时是怎么算的

| 来源 | 起点 | 终点 |
| --- | --- | --- |
| Claude Code | transcript 里最近一条**真实用户输入**（`tool_result` 不算，否则会严重低估） | 最后一条回复 |
| Codex | rollout 里最后一条 `task_started` | hook 触发时刻 |

任一来源取不到起点时耗时按未知处理，此时 `min-duration` 不参与判断——
否则会把该来源的通知全部挡掉。

## 开发

```bash
make test     # go vet + go test -race
make build    # 产出 ./bin/acn
```

Homebrew Formula 只存在于 tap 仓库 [windyskr/homebrew-tap](https://github.com/Windyskr/homebrew-tap)，
主仓库不留副本——两份会各自漂移。

一条硬约束：hook 进程**绝不能往 stdout 写任何东西**。
Codex 的 Stop hook 若在 stdout 收到 `{"decision":"block"}` 会自动续跑一轮。
所有诊断信息一律走 stderr。

### 曾经有过、后来删掉的

一个 Unix socket + 常驻 daemon，用于让 hook 写完就退出、把 HTTP 请求丢给后台做。
实测收益只有 78ms（两条路径的本地开销都是 45ms，差别仅是一次约 80ms 的飞书往返），
却要付出常驻服务、socket 生命周期、两套投递路径和服务管理的代价，因此移除。

连带删掉的还有按指纹去重——它防的是「daemon 与直发重复上报」，而那两条路径本就是
二选一，构造不出重复；反倒可能把两次内容相同的短任务通知误吞一条。

## 许可

MIT
