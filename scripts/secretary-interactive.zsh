#!/usr/bin/env zsh

terminal_available() {
  [[ -t 1 && -r /dev/tty ]]
}

terminal_read() {
  local prompt="$1" default="${2:-}"
  terminal_available || { print -u2 "Interactive configuration requires a terminal"; return 1; }
  print -n -- "$prompt [$default]: " >/dev/tty
  IFS= read -r REPLY </dev/tty || return 1
  [[ -n "$REPLY" ]] || REPLY="$default"
}

terminal_secret() {
  terminal_available || { print -u2 "Interactive configuration requires a terminal"; return 1; }
  print -n -- "$1: " >/dev/tty
  IFS= read -rs REPLY </dev/tty || return 1
  print "" >/dev/tty
}

native_harness_detected() {
  case "$1" in
    codex) path_command codex >/dev/null 2>&1 ;;
    claude_code) path_command claude >/dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}

choose_harness() {
  local default="$1" answer="" codex_ok=false claude_ok=false
  case "$(uname -s)/$(uname -m)" in Darwin/arm64|Darwin/x86_64|Linux/aarch64|Linux/x86_64) codex_ok=true ;; esac
  native_harness_detected claude_code && claude_ok=true
  if [[ "$codex_ok" == false && "$claude_ok" == false ]]; then
    print -u2 "Neither Codex nor Claude Code supported on this host."
    return 1
  fi
  [[ "$codex_ok" == true ]] || default="claude_code"
  [[ "$claude_ok" == true ]] || default="codex"
  if terminal_available; then
    print "Available: Codex=$codex_ok, Claude Code=$claude_ok"
    terminal_read "Secretary and Worker harness (codex/claude_code)" "$default" || return 1
    answer="$REPLY"
  else
    answer="$default"
  fi
  case "$answer" in
    codex) [[ "$codex_ok" == true ]] || { print -u2 "Codex is unavailable"; return 1; } ;;
    claude_code) [[ "$claude_ok" == true ]] || { print -u2 "Claude Code is unavailable"; return 1; } ;;
    *) print -u2 "Choose codex or claude_code"; return 2 ;;
  esac
  SELECTED_HARNESS="$answer"
}

set_config_harness() {
  local harness="$1" file="$STATE/config.toml" temp
  [[ "$harness" == codex || "$harness" == claude_code ]] || { print -u2 "Unsupported harness: $harness"; return 2; }
  [[ -f "$file" && ! -L "$file" ]] || { print -u2 "Missing or unsafe configuration: $file"; return 1; }
  temp="$(mktemp "$STATE/config.toml.XXXXXX")" || return 1
  if ! awk -v selected="$harness" '
    /^\[[^]]+\]/ { section=$0 }
    section=="[secretary]" && /^[[:space:]]*harness[[:space:]]*=/ { print "harness = \"" selected "\""; secretary=1; next }
    section=="[worker_policy]" && /^[[:space:]]*default_harness[[:space:]]*=/ { print "default_harness = \"" selected "\""; worker=1; next }
    { print }
    END { if (!secretary || !worker) exit 3 }
  ' "$file" > "$temp"; then
    rm -f "$temp"
    print -u2 "Config needs [secretary].harness and [worker_policy].default_harness; refusing to replace legacy sections"
    return 1
  fi
  chmod 600 "$temp"
  mv -f "$temp" "$file"
  print "Selected $harness for new Secretary and Worker sessions. Existing Workers are unchanged."
}

set_environment_value() {
  local key="$1" value="$2" temp
  [[ "$key" =~ '^SECRETARY_[A-Z0-9_]+$' ]] || return 2
  [[ "$value" != *"'"* && "$value" != *$'\n'* && "$value" != *$'\r'* ]] || return 2
  [[ -f "$ENV_FILE" && ! -L "$ENV_FILE" ]] || { print -u2 "Missing or unsafe private environment file"; return 1; }
  temp="$(mktemp "$STATE/environment.XXXXXX")" || return 1
  if ! awk -v key="$key" 'index($0, "export " key "=") != 1 {print}' "$ENV_FILE" > "$temp"; then
    rm -f "$temp"; return 1
  fi
  printf "export %s='%s'\n" "$key" "$value" >> "$temp"
  chmod 600 "$temp"
  mv -f "$temp" "$ENV_FILE"
}

ensure_local_node_credentials() {
  [[ -f "$ENV_FILE" && ! -L "$ENV_FILE" ]] || { print -u2 "Missing or unsafe private environment file"; return 1; }
  # Read from the persisted file only; inherited shell values must not mask a
  # missing credential in the service environment on an upgraded installation.
  local pair admin created=false
  pair="$( (unset SECRETARY_NODE_PAIRING_TOKEN SECRETARY_NODE_ADMIN_TOKEN; source "$ENV_FILE"; print -r -- "${SECRETARY_NODE_PAIRING_TOKEN:-}") )" || return 1
  admin="$( (unset SECRETARY_NODE_PAIRING_TOKEN SECRETARY_NODE_ADMIN_TOKEN; source "$ENV_FILE"; print -r -- "${SECRETARY_NODE_ADMIN_TOKEN:-}") )" || return 1
  if [[ -n "$pair" && -n "$admin" ]]; then
    [[ "$pair" != "$admin" ]] || { print -u2 "Local Node pairing and admin credentials must differ"; return 1; }
    return 0
  fi
  if [[ -z "$pair" ]]; then
    pair="$(openssl rand -hex 32)" || return 1
    set_environment_value SECRETARY_NODE_PAIRING_TOKEN "$pair" || return 1
    created=true
  fi
  if [[ -z "$admin" ]]; then
    admin="$(openssl rand -hex 32)" || return 1
    set_environment_value SECRETARY_NODE_ADMIN_TOKEN "$admin" || return 1
    created=true
  fi
  [[ "$pair" != "$admin" ]] || { print -u2 "Local Node credentials must differ"; return 1; }
  [[ "$created" == true ]] && print "Provisioned missing local Node credentials without rotating existing secrets."
}

telegram_origin_valid() {
  local origin="$1" host ip part
  [[ "$origin" == https://* && "$origin" != *[[:space:]]* && "$origin" != *[\?#@]* ]] || return 1
  local authority="${origin#https://}"
  [[ -n "$authority" && "$authority" != */* ]] || return 1
  host="$authority"
  if [[ "$host" == *:* ]]; then
    host="${authority%:*}"
    [[ "${authority##*:}" == <-> ]] || return 1
  fi
  [[ -n "$host" && "$host" != .* && "$host" != *..* ]] || return 1
  [[ "$host:l" != localhost && "$host:l" != *.localhost && "$host:l" != *.local ]] || return 1
  if [[ "$host" == <->.<->.<->.<-> ]]; then
    local -a octets
    octets=("${(@s:.:)host}")
    for part in "${octets[@]}"; do (( part >= 0 && part <= 255 )) || return 1; done
    (( octets[1] == 10 || octets[1] == 127 || octets[1] == 0 || octets[1] == 169 && octets[2] == 254 || octets[1] == 172 && octets[2] >= 16 && octets[2] <= 31 || octets[1] == 192 && octets[2] == 168 )) && return 1
  fi
  return 0
}

telegram_origin_doctor() {
  case "${SECRETARY_TELEGRAM_ENABLED:l}" in
    true|yes|on|1)
      telegram_origin_valid "${SECRETARY_TELEGRAM_WEB_BASE_URL:-}" || {
        print -u2 "Telegram configuration invalid: SECRETARY_TELEGRAM_WEB_BASE_URL must be a public HTTPS origin without credentials, path, query or fragment"
        return 1
      } ;;
  esac
}

configure_telegram() {
  local token="${SECRETARY_TELEGRAM_BOT_TOKEN:-}" chat="${SECRETARY_TELEGRAM_OWNER_CHAT_ID:-}" credential="${SECRETARY_TELEGRAM_SERVER_CREDENTIAL:-}" origin="${SECRETARY_TELEGRAM_WEB_BASE_URL:-}"
  terminal_available || { print -u2 "Telegram configuration requires a terminal"; return 1; }
  terminal_read "Enable Telegram DM (yes/no)" "${SECRETARY_TELEGRAM_ENABLED:-no}" || return 1
  case "${REPLY:l}" in
    no|n|false|0)
      set_environment_value SECRETARY_TELEGRAM_ENABLED false || return 1
      print "Telegram disabled; stored credentials preserved."
      return 0 ;;
    yes|y|true|1) ;;
    *) print -u2 "Answer yes or no"; return 2 ;;
  esac
  [[ -z "$token" ]] || print "A Telegram token exists. Leave blank to keep it."
  terminal_secret "BotFather bot token (blank keeps existing)" || return 1
  [[ -z "$REPLY" ]] || token="$REPLY"
  [[ "$token" =~ '^[0-9]+:[A-Za-z0-9_-]+$' ]] || { print -u2 "Expected a BotFather token (digits:secret); no changes made"; return 2; }
  terminal_read "Owner DM chat ID (optional)" "$chat" || return 1
  chat="$REPLY"
  [[ -z "$chat" || "$chat" =~ '^-?[0-9]+$' ]] || { print -u2 "Owner chat ID must be an integer"; return 2; }
  [[ "$chat" != 0 ]] || return 2
  terminal_read "Public Web origin for Telegram links (https://host)" "$origin" || return 1
  origin="${REPLY%/}"
  if ! telegram_origin_valid "$origin"; then
    print -u2 "Telegram requires a public HTTPS origin without credentials, path, query or fragment; no changes made"
    return 2
  fi
  [[ -n "$credential" ]] || credential="$(openssl rand -hex 32)" || return 1
  set_environment_value SECRETARY_TELEGRAM_BOT_TOKEN "$token" || return 1
  set_environment_value SECRETARY_TELEGRAM_SERVER_CREDENTIAL "$credential" || return 1
  [[ -z "$chat" ]] || set_environment_value SECRETARY_TELEGRAM_OWNER_CHAT_ID "$chat" || return 1
  set_environment_value SECRETARY_TELEGRAM_WEB_BASE_URL "$origin" || return 1
  set_environment_value SECRETARY_TELEGRAM_ENABLED true || return 1
  print "Telegram configured. Restart Secretary to apply."
}

interactive_config() {
  terminal_available || { print -u2 "secretary config requires a terminal"; return 1; }
  [[ -f "$STATE/config.toml" ]] || { print -u2 "Run secretary setup first"; return 1; }
  load_environment
  local selection current
  while true; do
    print ""
    print "Secretary configuration"
    print "  1) Native harness: $(configured_harness)"
    print "  2) Local Execution Node: $(node_config_value node 2>/dev/null || print not-configured)"
    print "  3) Telegram: ${SECRETARY_TELEGRAM_ENABLED:-false}"
    print "  4) Paths and operations"
    print "  5) Done"
    terminal_read "Select" 5 || return 1
    selection="$REPLY"
    case "$selection" in
      1)
        current="$(configured_harness)"
        choose_harness "$current" || return 1
        prepare_harness "$SELECTED_HARNESS" true || return 1
        set_config_harness "$SELECTED_HARNESS" || return 1 ;;
      2)
        print "Existing workspace mappings are retained unless explicitly changed with --workspace or --clear-workspaces."
        local old_server="$(node_config_value server_url)" old_name="$(node_config_value node)" old_capacity="$(node_config_value capacity)"
        local server name capacity
        terminal_read "Node server URL" "$old_server" || return 1
        server="$REPLY"
        terminal_read "Node name" "$old_name" || return 1
        name="$REPLY"
        terminal_read "Maximum parallel Workers" "$old_capacity" || return 1
        capacity="$REPLY"
        if [[ -f "$NODE_DATA/identity.json" && ( "$server" != "$old_server" || "$name" != "$old_name" ) ]]; then
          print -u2 "Paired Node identity cannot be silently changed; use explicit drain/revoke and re-enrollment."
          continue
        fi
        node_setup --server "$server" --name "$name" --capacity "$capacity" || return 1 ;;
      3)
        configure_telegram || return 1
        load_environment ;;
      4)
        print "Config: $STATE/config.toml"
        print "Private environment: $ENV_FILE"
        print "Node config: $NODE_CONFIG"
        print "Commands: secretary doctor | start | stop | restart | status | logs | update"
        print "Service: secretary install-service | uninstall-service" ;;
      5|q|Q) break ;;
      *) print "Invalid selection" ;;
    esac
  done
  print "Configuration saved. Restart running processes to apply changes."
}
