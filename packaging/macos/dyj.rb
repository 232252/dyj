# Homebrew formula for dyj (macOS). The CLI binary ships via formula —
# brew's download pipeline sets no quarantine attribute, so Gatekeeper
# does not apply and no notarization is needed (docs/research/10 §5.3).
# `brew services start dyj` runs it as a launchd job via the service
# block below.
class Dyj < Formula
  desc "LAN printer sharing, driverless (IPP Everywhere + mDNS)"
  homepage "https://github.com/sq199/dyj"
  version "0.2.0"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/sq199/dyj/releases/download/v#{version}/dyj-darwin-arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # mark: darwin-arm64
    else
      url "https://github.com/sq199/dyj/releases/download/v#{version}/dyj-darwin-amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # mark: darwin-amd64
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/sq199/dyj/releases/download/v#{version}/dyj-linux-arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # mark: linux-arm64
    else
      url "https://github.com/sq199/dyj/releases/download/v#{version}/dyj-linux-amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000" # mark: linux-amd64
    end
  end

  def install
    bin.install "dyj"
  end

  service do
    run [opt_bin/"dyj", "service", "run"]
    keep_alive true
    log_path "/var/log/dyj.log"
    error_log_path "/var/log/dyj.log"
  end

  def caveats
    <<~EOS
      Start sharing and open the admin page:
        brew services start dyj
        open http://localhost:6631/admin

      LAN access to the admin page needs the token from the state dir:
        cat /var/lib/dyj/admin-token   # Linux
      macOS state dir: /Library/Application Support/dyj
    EOS
  end

  test do
    # `dyj` with no args prints usage (exit 2 is fine for a CLI smoke test).
    assert_match "Usage", shell_output("#{bin}/dyj 2>&1 || true")
  end
end
