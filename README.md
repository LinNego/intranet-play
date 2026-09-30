# intranet-play

内网联机的终端小游戏合集。Go 编写，TCP + 一行一条 JSON，Host 权威、无中心服务器，同一网段开箱即玩。

| 游戏 | 人数 | 入口 |
|---|---|---|
| 五子棋 | 2 人 | `intranet-play host` / `intranet-play join` |
| 斗地主 | 3 人 | `intranet-play ddz host` / `intranet-play ddz join` |

UI 刻意做成"数据面板"的样子（`nx-sync`），在工位上不显眼。

---

## 快速开始

### 五子棋（2 人）

```powershell
# 房主
.\intranet-play.exe host -port 9988 -name Alice

# 对手
.\intranet-play.exe join -addr 192.168.1.23:9988 -name Bob
```

房主先按提示设定棋盘大小（10-20）与先手方，然后输入坐标落子。

命令：`x,y`（如 `7,7` 或 `a,2`）| `chat TEXT` | `resign` | `restart` | `leave`

### 斗地主（3 人）

**房主也占一个座位**，所以三个人各开一个终端：

```powershell
# 房主（座位 0）
.\intranet-play.exe ddz host -port 9988 -name Alice

# 玩家 2（座位 1）
.\intranet-play.exe ddz join -addr 192.168.1.23:9988 -name Bob

# 玩家 3（座位 2）
.\intranet-play.exe ddz join -addr 192.168.1.23:9988 -name Cara
```

三人到齐后，房主输入 `start` 开始。房间是严格的 3 人局，第 4 个连接会收到 `room full` 并被断开。

| 命令 | 作用 |
|---|---|
| `play 1 2 5` | 打出手牌编号 1、2、5 三张牌 |
| `play 1-3` | 区间写法，等价于 `play 1 2 3` |
| `bid 0` / `bid 1..3` | 叫分，`0` 表示不叫；叫 3 分立刻定地主 |
| `pass` | 不要（拥有自由出牌权时必须出牌，不能 pass） |
| `next` | 本局结束后开下一局（房主或任一玩家都可发起） |
| `start` | 房主开局 / 打完一场后重开 |
| `chat TEXT` | 聊天 |
| `help` | 命令帮助 |
| `leave` | 退出房间 |

规则要点：

- 一副 54 张，每人 17 张 + 3 张底牌；叫分 1/2/3 定地主，底牌归地主且三家可见
- 牌型：单张、对子、三张、三带一、三带二、顺子(≥5)、连对(≥3 对)、飞机、飞机带单、飞机带对、四带两单、四带两对、炸弹、王炸；顺子/连对/飞机不含 2 和王
- 炸弹与王炸每出现一次倍数 ×2；春天 / 反春天再 ×2
- 单局分 = 底分 × 叫分 × 倍数；地主赢则地主 +2 倍、每个农民 −1 倍，反之亦然
- 默认打 10 局（`-rounds` 可改，`-rounds 0` 不限局数），`-base` 调底分
- 3 人局无法托管：有人掉线本局立即作废回大厅，房主重新 `start`

牌面里 `w` / `W` 是大小王，`♠♥♣♦` 是花色；手牌按牌力降序编号，出牌后重新编号。

---

## 连不上怎么办

1. **确认地址**：`join` 要填房主机器的**局域网 IP**（`ipconfig` / `ip addr` 查看），不是 `127.0.0.1`（除非同一台机器开多个终端）
2. **放行端口**：Windows 防火墙首次运行会弹窗，要允许"专用网络"；或手动放行 TCP 9988
3. **同一网段**：确认两台机器在同一 Wi-Fi / 交换机下，`ping` 得通
4. **中文与颜色**：建议用 Windows Terminal；老 conhost 下如需手动切码页，执行 `chcp 65001`

---

## 开发

```powershell
go test ./...        # 全部测试
go test ./... -race  # 并发检查（含三人联机端到端测试）
go vet ./...
go build ./cmd/intranet-play
```

目录结构：

```text
cmd/intranet-play/      命令行入口（host / join / ddz）
internal/protocol/      一行一条 JSON 的消息定义（五子棋 + DDZ_*）
internal/netx/          TCP 连接封装（inbox channel + 写锁）
internal/game/          五子棋棋盘引擎
internal/ddz/           斗地主规则引擎（牌型判定、比较、对局状态机）
internal/room/          房间编排：五子棋 2 人；ddz_* 为 3 人斗地主
internal/ui/            终端界面（bubbletea）：tui.go 五子棋，ddz.go 斗地主
docs/开发计划.md          五子棋开发计划
docs/斗地主开发计划.md     斗地主开发计划与实施进度
```

设计约定：

- **权威只在房主**：客户端只发命令、只渲染收到的视图，绝不本地推演
- **手牌只单播**：公共状态里只有张数，任何人的手牌明文只发给本人；这条红线上有专门的测试兜着
- **出牌用 cardID**：客户端上报的是手牌编号对应的牌 ID，房主校验归属，无法伪造
- **协议向后兼容**：未知 `type` 一律忽略，方便以后加字段
