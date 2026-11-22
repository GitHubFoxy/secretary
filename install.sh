#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -eq 0 ]; then
  if [ -z "${SUDO_USER:-}" ] || [ "$SUDO_USER" = root ]; then
    echo 'Запустите установку от обычного пользователя, не от root.' >&2
    exit 1
  fi
  exec sudo -u "$SUDO_USER" -H env XDG_RUNTIME_DIR="/run/user/$(id -u "$SUDO_USER")" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$(id -u "$SUDO_USER")/bus" bash "$0" "$@"
fi

if [ "$(uname -m)" != x86_64 ] || [ "$(uname -s)" != Linux ]; then
  echo 'Поддерживаются только Arch и Ubuntu 24.04 на x86_64.' >&2; exit 1
fi
. /etc/os-release
if [ "$ID" != arch ] && { [ "$ID" != ubuntu ] || [ "${VERSION_ID:-}" != 24.04 ]; }; then
  echo 'Поддерживаются только Arch и Ubuntu 24.04 на x86_64.' >&2; exit 1
fi

printf 'Подписка: 1 — Claude, 2 — Codex, 3 — обе: '
read -r choice
case "$choice" in 1|2|3) ;; *) echo 'Неизвестный выбор.' >&2; exit 1;; esac
sudo -v
if [ "$ID" = arch ]; then
  sudo pacman -S --needed --noconfirm curl git tar gzip unzip nodejs npm tailscale
  sudo systemctl enable --now tailscaled
else
  sudo apt-get update
  sudo apt-get install -y curl git tar gzip unzip nodejs npm ca-certificates
  if ! command -v tailscale >/dev/null; then curl -fsSL https://tailscale.com/install.sh | sh; fi
  sudo systemctl enable --now tailscaled
fi

mkdir -p "$HOME/.secretary/releases" "$HOME/.local/bin" "$HOME/.config/systemd/user"
export PATH="$HOME/.secretary/bun/bin:$HOME/.local/bin:$PATH"
if [ ! -x "$HOME/.secretary/bun/bin/bun" ]; then
  curl -fsSL https://bun.sh/install | BUN_INSTALL="$HOME/.secretary/bun" bash -s 'bun-v1.3.5'
fi
if [ "$choice" = 1 ] || [ "$choice" = 3 ]; then
  if ! command -v claude >/dev/null; then curl -fsSL https://claude.ai/install.sh | bash; fi
  claude auth login
fi
if [ "$choice" = 2 ] || [ "$choice" = 3 ]; then
  if ! command -v codex >/dev/null; then npm install --prefix "$HOME/.local" -g @openai/codex; fi
  codex login --device-auth
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl --progress-bar -fL https://github.com/GitHubFoxy/secretary/releases/latest/download/secretary.tar.gz -o "$tmp/secretary.tar.gz"
curl --progress-bar -fL https://github.com/GitHubFoxy/secretary/releases/latest/download/secretary.tar.gz.sha256 -o "$tmp/secretary.tar.gz.sha256"
(cd "$tmp" && sha256sum -c secretary.tar.gz.sha256)
release="$HOME/.secretary/releases/$(date +%Y%m%d%H%M%S)"
mkdir "$release"
tar -xzf "$tmp/secretary.tar.gz" -C "$release"
ln -sfn "$release" "$HOME/.secretary/current"
ln -sfn "$HOME/.secretary/current/secretary" "$HOME/.local/bin/secretary"
cat > "$HOME/.config/systemd/user/secretary.service" <<'UNIT'
[Unit]
Description=Secretary
After=network-online.target
[Service]
WorkingDirectory=%h/.secretary/current/server
Environment=PATH=%h/.secretary/bun/bin:%h/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=%h/.secretary/bun/bin/bun index.ts
Restart=on-failure
KillMode=control-group
[Install]
WantedBy=default.target
UNIT
sudo loginctl enable-linger "$(id -un)"
systemctl --user daemon-reload
systemctl --user enable --now secretary.service
sudo tailscale up
sudo tailscale serve --bg 3000
address=$(tailscale status --json | "$HOME/.secretary/bun/bin/bun" -e 'let s=""; for await (const c of Bun.stdin.stream()) s+=new TextDecoder().decode(c); console.log(JSON.parse(s).Self.DNSName.replace(/\.$/, ""))')
echo "Host: https://$address"
"$HOME/.local/bin/secretary" pair
printf '\nДля полного доступа Secretary можно отключить пароль sudo строкой в /etc/sudoers.d/secretary:\n%s ALL=(ALL) NOPASSWD: ALL\nДоступ к Secretary тогда означает root на Host.\n' "$(id -un)"
