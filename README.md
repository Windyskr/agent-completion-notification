# acn

AI CLI 任务完成通知。Claude Code / Codex 跑完一轮，推送到飞书。

单个 Go 二进制，零依赖，无界面。事件驱动——不轮询日志、不猜测状态。

## 原理

两个 CLI 都原生支持任务结束回调，acn 只是接上它们：

```
Claude Code ──Stop hook（stdin JSON）──┐
                                       ├─→ acn hook ─→ unix socket ─→ acn daemon ─→ 飞书
Codex ───────notify 回调（argv JSON）──┘              （daemon 未运行时 hook 自行直发）
```

daemon 的存在只有一个理由：Claude Code 的 Stop hook 会**阻塞 CLI 返回**。
hook 写完 socket 就退出（实测 ~20ms），HTTP 请求丢给 daemon 异步做。
不启动 daemon 也能用，只是每次通知会多等一次网络往返。

## 安装

```bash
brew install windyskr/tap/acn      # 或：go install github.com/windyskr/acn/cmd/acn@latest

acn config webhook https://open.feishu.cn/open-apis/bot/v2/hook/xxxx
acn install
brew services start acn
```

`acn install` 会改写这两个文件（**改写前自动备份为 `*.acn.bak`**）：

| 文件 | 改动 |
| --- | --- |
| `~/.claude/settings.json` | 增加一个 `Stop` hook |
| `~/.codex/config.toml` | 接管顶层 `notify` |

Claude Code 与 Codex 需重启后生效。

### 关于 Codex 的 notify 冲突

Codex 只允许配一个 `notify` 程序。如果你已经装了别的集成（如 Codex Computer Use），
acn 会把原有配置存下来并在每次回调时**原样转发**，不会弄坏它。`acn uninstall` 时原样还原。

## 命令

```
acn install            接入 Claude Code 与 Codex
acn uninstall          摘除接入并还原 Codex 原有的 notify
acn status             查看接入状态、配置与 daemon 运行情况
acn config <k> <v>     修改配置
acn test               发送一条测试通知
acn daemon             前台运行常驻服务（brew services 调用）
```

## 配置

配置在 `~/.config/acn/config.json`（权限 0600），改完即时生效，无需重启 daemon。

```
acn config webhook <url>        飞书自定义机器人地址
acn config secret <str>         签名密钥（机器人未开启签名校验则不用配）
acn config min-duration <秒>    低于该耗时不推送，0 为不限
acn config claude <on|off>      是否推送 Claude Code
acn config codex <on|off>       是否推送 Codex
```

环境变量 `ACN_FEISHU_WEBHOOK_URL`、`ACN_FEISHU_SECRET` 优先级高于配置文件。
`ACN_CONFIG_DIR` 可改配置目录。

### 耗时阈值只对 Claude Code 生效

Claude Code 的耗时由 transcript 里「最近一条真实用户输入」到「最后一条回复」算出
（tool_result 不算用户输入，否则耗时会被严重低估）。
Codex 的 notify 载荷不含起始时间，耗时未知，因此 `min-duration` 对它不生效——
否则会把 Codex 的通知全部挡掉。

## 开发

```bash
make test     # go vet + go test -race
make build    # 产出 ./bin/acn
```

## 许可

MIT
