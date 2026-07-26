# Homebrew Formula。放在 tap 仓库里即可 brew install，或本地：
#   brew install --build-from-source ./Formula/acn.rb
class Acn < Formula
  desc "Agent Completion Notification - AI CLI 任务完成通知（Claude Code / Codex → 飞书）"
  homepage "https://github.com/windyskr/acn"
  url "https://github.com/windyskr/acn/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "MIT"
  head "https://github.com/windyskr/acn.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/acn"
  end


  def caveats
    <<~EOS
      接入 Claude Code 与 Codex（会自动备份两者的配置）：
        acn config webhook <飞书机器人地址>
        acn install
        acn doctor

      Claude Code 与 Codex 需重启后生效。查看状态：acn status
    EOS
  end

  test do
    assert_match "acn", shell_output("#{bin}/acn version")

    # 任何输入下 hook 都必须静默退出——否则会污染 AI CLI 的终端输出。
    ENV["ACN_CONFIG_DIR"] = testpath/"config"
    system bin/"acn", "hook", "claude" # stdin 为空，应当正常退出
  end
end
