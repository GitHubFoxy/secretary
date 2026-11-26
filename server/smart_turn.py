import base64
import io
import json
import os
import sys
import time
import wave

os.environ['OPENBLAS_NUM_THREADS'] = '1'

import numpy as np
import onnxruntime as ort
from whisper_features import compute_whisper_log_mel_features

options = ort.SessionOptions()
options.intra_op_num_threads = 1
options.inter_op_num_threads = 1
session = ort.InferenceSession(os.path.expanduser('~/.secretary/asr/smart-turn-v3.2-cpu.onnx'), sess_options=options, providers=['CPUExecutionProvider'])

for line in sys.stdin:
    try:
        request = json.loads(line)
        started = time.monotonic()
        with wave.open(io.BytesIO(base64.b64decode(request['wav'], validate=True))) as audio:
            if audio.getnchannels() != 1 or audio.getsampwidth() != 2 or audio.getframerate() != 16000:
                raise ValueError('Expected 16 kHz mono PCM16 WAV')
            samples = np.frombuffer(audio.readframes(audio.getnframes()), dtype='<i2').astype(np.float32) / 32768
        samples = samples[-128000:]
        samples = np.pad(samples, (128000 - len(samples), 0))
        features = compute_whisper_log_mel_features(samples)[None, :, :]
        probability = float(session.run(None, {'input_features': features})[0][0].item())
        result = {'id': request['id'], 'complete': probability > 0.5, 'ms': round((time.monotonic() - started) * 1000, 1)}
    except Exception as error:
        result = {'id': request.get('id') if 'request' in locals() else None, 'error': str(error)}
    print(json.dumps(result), flush=True)
