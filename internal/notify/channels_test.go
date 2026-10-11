package notify

import (
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
)

// 注册表是渠道的唯一清单：名称必须与配置开关、渠道自身的 Name() 一致，
// 否则开关、status 与投递结果会对不上。
func TestChannelsAreConsistent(t *testing.T) {
	defaults := config.Default()
	seen := map[string]bool{}
	for _, channel := range Channels {
		if seen[channel.Name] {
			t.Errorf("渠道 %q 重复登记", channel.Name)
		}
		seen[channel.Name] = true
		if _, ok := defaults.Channels[channel.Name]; !ok {
			t.Errorf("渠道 %q 不在默认配置的渠道开关里", channel.Name)
		}
		if channel.Label == "" || channel.Setup == "" || channel.Ready == nil || channel.Describe == nil || channel.New == nil {
			t.Errorf("渠道 %q 登记不完整: %+v", channel.Name, channel)
			continue
		}
		if got := channel.New(defaults).Name(); got != channel.Name {
			t.Errorf("渠道 %q 构造出的 Name() = %q", channel.Name, got)
		}
		if channel.Ready(config.Config{}) {
			t.Errorf("渠道 %q 空配置时不应就绪", channel.Name)
		}
	}
	if len(seen) != len(defaults.Channels) {
		t.Errorf("注册表有 %d 个渠道，默认配置有 %d 个开关", len(seen), len(defaults.Channels))
	}
}

func TestDeliveryReadyRequiresConfiguredEnabledChannel(t *testing.T) {
	cfg := config.Default()
	if DeliveryReady(cfg) {
		t.Error("未配置渠道时不应可投递")
	}
	cfg.Bark.URL = "https://api.day.app/key"
	if !DeliveryReady(cfg) {
		t.Error("已配置 Bark 时应可投递")
	}
	cfg.Channels[config.ChannelBark] = false
	if DeliveryReady(cfg) {
		t.Error("Bark 已禁用时不应可投递")
	}
	cfg.Telegram.BotToken = "123:abc"
	if DeliveryReady(cfg) {
		t.Error("Telegram 缺少 chat_id 时不应可投递")
	}
	cfg.Telegram.ChatID = "42"
	if !DeliveryReady(cfg) {
		t.Error("Telegram 配置完整时应可投递")
	}
}

func TestDescribeChannels(t *testing.T) {
	cfg := config.Config{
		Feishu:   config.Feishu{WebhookURL: " https://open.feishu.cn/hook/key ", Secret: "s"},
		Telegram: config.Telegram{BotToken: "1234567890:abcdef"},
	}
	cases := map[string]string{
		config.ChannelFeishu:   "https://open.feishu.cn/hook/key（已启用签名校验）",
		config.ChannelDingTalk: "未配置",
		config.ChannelTelegram: "1234…cdef（缺少 chat_id）",
		config.ChannelEmail:    "未完整配置",
	}
	for _, channel := range Channels {
		if want, ok := cases[channel.Name]; ok {
			if got := channel.Describe(cfg); got != want {
				t.Errorf("%s: Describe = %q, 期望 %q", channel.Name, got, want)
			}
		}
	}
}
