#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != Linux ] || [ "$(uname -m)" != x86_64 ]; then
  echo 'Поддерживается Linux x86_64.' >&2; exit 1
fi
mkdir -p "$HOME/.secretary"
tmp=$(mktemp -d "$HOME/.secretary/asr-install.XXXXXXXX")
trap 'rm -rf "$tmp"' EXIT
binary=sherpa-onnx-v2.0.2-linux-x64-static-no-tts.tar.bz2
model=sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8.tar.bz2
curl --progress-bar -fL "https://github.com/k2-fsa/sherpa-onnx/releases/download/v2.0.2/$binary" -o "$tmp/$binary"
curl --progress-bar -fL "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/$model" -o "$tmp/$model"
(cd "$tmp" && printf '%s  %s\n' af62ba430576c8d8963a142417f9665e61f29df9704e40627b286ca6520d0b29 "$binary" 1495e91b59d7fc53ddca91f3431758ec99f8fa253fdb0e1e851be0f72b14b0f9 "$model" | sha256sum -c)
tar -xjf "$tmp/$binary" -C "$tmp"
tar -xjf "$tmp/$model" -C "$tmp"
mkdir -p "$HOME/.secretary/asr"
cp "$tmp/sherpa-onnx-v2.0.2-linux-x64-static-no-tts/bin/sherpa-onnx-offline" "$HOME/.secretary/asr/"
modeldir="$tmp/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8"
cp "$modeldir/tokens.txt" "$HOME/.secretary/asr/"
for part in encoder decoder joiner; do cp "$modeldir/$part.int8.onnx" "$HOME/.secretary/asr/$part.onnx"; done
echo 'Распознавание речи установлено.'
