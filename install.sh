#!/usr/bin/env bash
set -euo pipefail

# An agent or a script passes the choice as an argument; a person answers the question.
usage='Использование: install.sh [--subscription claude|codex|both] [--private]'
choice='' private=''
while [ $# -gt 0 ]; do
  case "$1" in
    --subscription) case "${2:-}" in claude) choice=1;; codex) choice=2;; both) choice=3;; *) echo "$usage" >&2; exit 1;; esac; shift 2;;
    --private) private=1; shift;;
    -h|--help) echo "$usage"; exit 0;;
    *) echo "$usage" >&2; exit 1;;
  esac
done

# Secretary and its agents run as an ordinary user; root here is a mistake to explain, not to work around.
if [ "$(id -u)" -eq 0 ]; then
  cat >&2 <<'ROOT'
Secretary ставится от обычного пользователя, не от root.
Создайте пользователя и войдите под ним:

  adduser owner
  usermod -aG sudo owner   # на Arch: usermod -aG wheel owner
  su - owner

Затем запустите установку снова, без sudo.
ROOT
  exit 1
fi

if [ "$(uname -m)" != x86_64 ] || [ "$(uname -s)" != Linux ]; then
  echo 'Поддерживаются только Arch и Ubuntu 24.04 на x86_64.' >&2; exit 1
fi
. /etc/os-release
if [ "$ID" != arch ] && { [ "$ID" != ubuntu ] || [ "${VERSION_ID:-}" != 24.04 ]; }; then
  echo 'Поддерживаются только Arch и Ubuntu 24.04 на x86_64.' >&2; exit 1
fi

# Each step prints its number and lands in the log with its duration, so a slow or failed step is visible.
mkdir -p "$HOME/.secretary"
log="$HOME/.secretary/install.log"
total=8 n=0 name='' started=$SECONDS
finish() { [ -n "$name" ] && printf '%s\t%s\t%ss\n' "$(date -Is)" "$name" "$((SECONDS - started))" >> "$log"; name=''; }
step() { finish; n=$((n + 1)); name=$1; started=$SECONDS; printf '\n[%d/%d] %s\n' "$n" "$total" "$name"; }
trap 'printf "\nУстановка остановилась на шаге «%s». Журнал: %s\n" "$name" "$log" >&2; printf "%s\t%s\tошибка\n" "$(date -Is)" "$name" >> "$log"' ERR
printf '%s\tначало\t%s %s\n' "$(date -Is)" "$ID" "${VERSION_ID:-}" >> "$log"

step 'Выбор подписки'
if [ -z "$choice" ]; then
  # With `curl … | bash` stdin is the script itself, so the answer is read from the terminal.
  if ! { : < /dev/tty; } 2>/dev/null; then echo "Нет терминала для вопроса. $usage" >&2; exit 1; fi
  printf 'Подписка: 1 — Claude, 2 — Codex, 3 — обе: '
  read -r choice < /dev/tty
  case "$choice" in 1|2|3) ;; *) echo 'Неизвестный выбор.' >&2; exit 1;; esac
fi
step 'Системные пакеты и Tailscale'
echo 'Обычно занимает до минуты.'
sudo -v
if [ "$ID" = arch ]; then
  sudo pacman -S --needed --noconfirm curl git tar gzip unzip tailscale
  sudo systemctl enable --now tailscaled
else
  # needrestart on Ubuntu 24.04 otherwise stops apt with a full-screen dialog.
  sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get update
  sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install -y curl git tar gzip unzip ca-certificates
  if ! command -v tailscale >/dev/null; then curl -fsSL https://tailscale.com/install.sh | sh; fi
  sudo systemctl enable --now tailscaled
fi

step 'Bun'
mkdir -p "$HOME/.secretary/releases" "$HOME/.local/bin" "$HOME/.config/systemd/user"
export PATH="$HOME/.secretary/bun/bin:$HOME/.local/bin:$PATH"
if [ ! -x "$HOME/.secretary/bun/bin/bun" ]; then
  curl -fsSL https://bun.sh/install | BUN_INSTALL="$HOME/.secretary/bun" bash -s 'bun-v1.3.5'
fi
step 'Агент и вход в подписку'
if [ "$choice" = 1 ] || [ "$choice" = 3 ]; then
  if ! command -v claude >/dev/null; then curl -fsSL https://claude.ai/install.sh | bash; fi
  claude auth login
fi
if [ "$choice" = 2 ] || [ "$choice" = 3 ]; then
  # The latest release binary: npm from the distribution would pull in hundreds of packages for one program.
  if ! command -v codex >/dev/null; then
    curl --progress-bar -fL https://github.com/openai/codex/releases/latest/download/codex-x86_64-unknown-linux-musl.tar.gz | tar -xzO > "$HOME/.local/bin/codex"
    chmod +x "$HOME/.local/bin/codex"
  fi
  codex login --device-auth
fi

step 'Загрузка Secretary'
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
step 'Служба Secretary'
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
step 'Подключение к Tailscale'
# tailscale up prints only its English login link and then nothing, so the script says what to do and confirms.
if ! tailscale status >/dev/null 2>&1; then
  echo 'Откройте ссылку ниже в браузере и войдите в Tailscale. Установка продолжится сама.'
fi
sudo tailscale up
# Funnel publishes Host on the internet, so the phone needs no VPN; the pairing code and device token guard it.
if [ -n "$private" ]; then
  sudo tailscale serve --bg 3000 >/dev/null
else
  echo 'Если Tailscale попросит включить Funnel, откройте ссылку и подтвердите: так телефон откроет Host без VPN.'
  sudo tailscale funnel --bg 3000
fi
address=$(tailscale status --json | "$HOME/.secretary/bun/bin/bun" -e 'let s=""; for await (const c of Bun.stdin.stream()) s+=new TextDecoder().decode(c); console.log(JSON.parse(s).Self.DNSName.replace(/\.$/, ""))')
echo '✓ Tailscale подключён'

step 'Подключение телефона'
code=$("$HOME/.local/bin/secretary" pair | grep -o '[A-Z0-9]\{4\}-[A-Z0-9]\{4\}')
finish
printf '%s\tготово\t%ss всего\n' "$(date -Is)" "$SECONDS" >> "$log"
printf '\nДля полного доступа Secretary можно отключить пароль sudo строкой в /etc/sudoers.d/secretary:\n%s ALL=(ALL) NOPASSWD: ALL\nДоступ к Secretary тогда означает root на Host.\n' "$(id -un)"
if [ -n "$private" ]; then
  phone='  На телефоне:
  1. Установите Tailscale и войдите тем же аккаунтом.
  2. Откройте Secretary и введите адрес Host и код.'
else
  phone='  На телефоне откройте Secretary и введите адрес Host и код.
  Host доступен из интернета; войти можно только по коду сопряжения.'
fi
cat <<DONE

════════════════════════════════════════════════════════
  ✓ Secretary установлен за $((SECONDS / 60)) мин $((SECONDS % 60)) с

  Адрес Host:  https://$address
  Код:         $code   (действует 10 минут; новый — secretary pair)

$phone
════════════════════════════════════════════════════════
DONE
