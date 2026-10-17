# Homebrew formula for dyj (macOS). The CLI binary ships via formula —
# brew's download pipeline sets no quarantine attribute, so Gatekeeper
# does not apply and no notarization is needed (docs/research/10 §5.3).
# `brew services start dyj` runs it as a launchd job via the service
# block below.
class Dyj < Formula
  desc "LAN printer sharing, driverless (IPP Everywhere + mDNS)"
  homepage "https://github.com/232252/dyj"
  # Tracks the latest published GitHub Release, not the in-tree version.
  # Tagging v0.4.2 refreshes these sha256 marks (see .github/workflows/release.yml).
  version "0.4.1"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-darwin-arm64.tar.gz"
      sha256 "687d1291299fb8ab7a980c4f47608fe707123461dde91fa80e50ea663afdb3c8" # mark: darwin-arm64
    else
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-darwin-amd64.tar.gz"
      sha256 "cc0c4c6bf97cdd8a5220586a7b510cfebd4e369ae3a1cf63a8ba287d9b9ca4e5" # mark: darwin-amd64
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-linux-arm64.tar.gz"
      sha256 "748d4e2185665a374273b0a80862c5747262f031d6a53e7b91d7ad95360aaa40" # mark: linux-arm64
    else
      url "https://github.com/232252/dyj/releases/download/v#{version}/dyj-linux-amd64.tar.gz"
      sha256 "2d4d324b7c5444ec23997d67f17e06fd89712cf4810f13a6c5b33d5dc2c373ac" # mark: linux-amd64
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
