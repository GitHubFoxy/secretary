#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != Linux ] || [ "$(uname -m)" != x86_64 ]; then
  echo 'Поддерживается Linux x86_64.' >&2; exit 1
fi
mkdir -p "$HOME/.secretary"
tmp=$(mktemp -d "$HOME/.secretary/tts-install.XXXXXXXX")
trap 'rm -rf "$tmp"' EXIT
binary=piper_linux_x86_64.tar.gz
model=ru_RU-dmitri-medium.onnx
revision=375a0fe641dea077c2a47b4e9a056d6da521eed3
curl --progress-bar -fL "https://github.com/rhasspy/piper/releases/download/2023.11.14-2/$binary" -o "$tmp/$binary"
for file in "$model" "$model.json"; do
  curl --progress-bar -fL "https://huggingface.co/rhasspy/piper-voices/resolve/$revision/ru/ru_RU/dmitri/medium/$file" -o "$tmp/$file"
done
(cd "$tmp" && sha256sum -c <<'CHECKSUMS'
a50cb45f355b7af1f6d758c1b360717877ba0a398cc8cbe6d2a7a3a26e225992  piper_linux_x86_64.tar.gz
f073356ebc4bd0f80c5af58df2953a5988bd5bdab1eb38635ce960b071fbefcb  ru_RU-dmitri-medium.onnx
667ef3117bc642c2892dff7690d8bdc8ca4228aeaa783b2dc1416df632855e0d  ru_RU-dmitri-medium.onnx.json
CHECKSUMS
)
tar -xzf "$tmp/$binary" -C "$tmp"
mkdir -p "$HOME/.secretary/tts"
cp -a "$tmp/piper" "$tmp/$model" "$tmp/$model.json" "$HOME/.secretary/tts/"
echo 'Синтез речи Piper установлен.'
