# Homebrew formula for dyj (macOS). The CLI binary ships via formula —
# brew's download pipeline sets no quarantine attribute, so Gatekeeper
# does not apply and no notarization is needed (docs/research/10 §5.3).
# `brew services start dyj` runs it as a launchd job via the service
# block below.
class Dyj < Formula
  desc "LAN printer sharing, driverless (IPP Everywhere + mDNS)"
  homepage "https://github.com/232252/dyj"
  # Tracks the latest published GitHub Release, not the in-tree version.
  # Each release tag refreshes these sha256 marks (see .github/workflows/release.yml).
  version "0.4.4"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-darwin-arm64.tar.gz"
      sha256 "dba495993897a5f8d66b1c7c91e945d7fc3ebb353efb196c005da4e993868aa8" # mark: darwin-arm64
    else
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-darwin-amd64.tar.gz"
      sha256 "854198f2b944b5f203a95e3d1cd6a028241ec0ff1697565c6aab58920aac8324" # mark: darwin-amd64
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-linux-arm64.tar.gz"
      sha256 "f6f445e93f9f156bc644cb7f95d499345506e80ca16def993209268ea04aca4a" # mark: linux-arm64
    else
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-linux-amd64.tar.gz"
      sha256 "f5bc9031a7188821c6bbb21463afc679a60b063f651df9c9722d5b923e138037" # mark: linux-amd64
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
