# acn

AI CLI 任务完成通知。Claude Code / Codex 跑完一轮，推送到飞书。

单个 Go 二进制，零依赖，无界面。事件驱动——不轮询日志、不猜测状态。

## 原理

两个 CLI 都原生支持 `Stop` 生命周期 hook，且**载荷 schema 几乎一致**，acn 只是接上它们：

```
Claude Code ──Stop hook──┐
                         ├─→ acn hook ─→ unix socket ─→ acn daemon ─→ 飞书
Codex ───────Stop hook───┘              （daemon 未运行时 hook 自行直发）
```

daemon 的存在只有一个理由：Stop hook 会**阻塞 CLI 返回**。
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

需要 Codex ≥0.129（`hooks` 是默认开启的特性）。

## 命令

```
acn install            接入 Claude Code 与 Codex
acn uninstall          摘除接入
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

一条硬约束：hook 进程**绝不能往 stdout 写任何东西**。
Codex 的 Stop hook 若在 stdout 收到 `{"decision":"block"}` 会自动续跑一轮。
所有诊断信息一律走 stderr。

## 许可

MIT
