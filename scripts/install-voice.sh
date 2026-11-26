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
revision=6888903da215c7735f51101d939f3bfa679fb2b8
mkdir "$tmp/gigaam"
for file in gigaam_v3_e2e_rnnt_encoder_int8.onnx gigaam_v3_e2e_rnnt_decoder.onnx gigaam_v3_e2e_rnnt_joint.onnx gigaam_v3_e2e_rnnt_tokens.txt; do
  curl --progress-bar -fL "https://huggingface.co/Smirnov75/GigaAM-v3-sherpa-onnx/resolve/$revision/$file" -o "$tmp/gigaam/$file"
done
(cd "$tmp/gigaam" && sha256sum -c <<'EOF'
2cac62d0c270bd128f898f2be1a2d34780d524a6e9483888ebac7b00f97410f1  gigaam_v3_e2e_rnnt_encoder_int8.onnx
781971998e6a355d6a714f6932a30eab295e7ba0d14fd7e0f78c83b87e811860  gigaam_v3_e2e_rnnt_decoder.onnx
602ff7017a93311aad34df1437c8d7f49911353c13d6eae7a6ee7b041339465c  gigaam_v3_e2e_rnnt_joint.onnx
7ddf22514c42c531358182c81446a8159771e9921019f09ae743ea622d40221d  gigaam_v3_e2e_rnnt_tokens.txt
EOF
)
(cd "$tmp" && printf '%s  %s\n' af62ba430576c8d8963a142417f9665e61f29df9704e40627b286ca6520d0b29 "$binary" 1495e91b59d7fc53ddca91f3431758ec99f8fa253fdb0e1e851be0f72b14b0f9 "$model" | sha256sum -c)
tar -xjf "$tmp/$binary" -C "$tmp"
tar -xjf "$tmp/$model" -C "$tmp"
mkdir -p "$HOME/.secretary/asr/parakeet" "$HOME/.secretary/asr/gigaam"
cp "$tmp/sherpa-onnx-v2.0.2-linux-x64-static-no-tts/bin/sherpa-onnx-offline" "$HOME/.secretary/asr/"
modeldir="$tmp/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8"
cp "$modeldir/tokens.txt" "$HOME/.secretary/asr/parakeet/"
for part in encoder decoder joiner; do cp "$modeldir/$part.int8.onnx" "$HOME/.secretary/asr/parakeet/$part.onnx"; done
cp "$tmp/gigaam/"* "$HOME/.secretary/asr/gigaam/"
smart_model=smart-turn-v3.2-cpu.onnx
smart_sha=2bb026316b14a660486a75b1733cd3fbab8c2fd0314dc9af7be49f8cca967e4f
smart_revision=f766f81d3cfdf7737ac64aad813d91bbfd56bf93
smart_folder="$HOME/.secretary/asr"
if ! printf '%s  %s\n' "$smart_sha" "$smart_folder/$smart_model" | sha256sum -c --status 2>/dev/null; then
  curl --progress-bar -fL "https://huggingface.co/pipecat-ai/smart-turn-v3/resolve/$smart_revision/$smart_model" -o "$tmp/$smart_model"
  printf '%s  %s\n' "$smart_sha" "$tmp/$smart_model" | sha256sum -c
  cp "$tmp/$smart_model" "$smart_folder/$smart_model"
fi
python3 -m venv "$smart_folder/smart-turn-venv"
"$smart_folder/smart-turn-venv/bin/python" -m pip install numpy onnxruntime
"$smart_folder/smart-turn-venv/bin/python" -c 'import numpy, onnxruntime'
echo 'Распознавание речи и Smart Turn установлены.'
