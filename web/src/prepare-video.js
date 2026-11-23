import { duration, wavFromRecording } from './voice.js';

function event(target, success, failure = 'error') {
  return new Promise((resolve, reject) => {
    const done = () => { target.removeEventListener(failure, fail); resolve(); };
    const fail = () => { target.removeEventListener(success, done); reject(new Error('Не удалось прочитать видео. Выберите другой формат.')); };
    target.addEventListener(success, done, {once:true});
    target.addEventListener(failure, fail, {once:true});
  });
}

export async function prepareVideo(file, interval) {
  const video = document.createElement('video');
  const url = URL.createObjectURL(file);
  video.preload = 'auto'; video.muted = true; video.src = url;
  try {
    await event(video, 'loadedmetadata');
    if (!Number.isFinite(video.duration) || video.duration <= 0) throw new Error('Не удалось определить длительность видео');
    if (video.duration > 60) throw new Error('Видео должно быть не длиннее 60 секунд');
    const seconds = video.duration;
    const scale = Math.min(1, 768 / Math.max(video.videoWidth, video.videoHeight));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(video.videoWidth * scale));
    canvas.height = Math.max(1, Math.round(video.videoHeight * scale));
    const frames = [];
    for (let time = 0; time < seconds; time += interval) {
      if (time === 0) { if (video.readyState < 2) await event(video, 'loadeddata'); }
      else { video.currentTime = time; await event(video, 'seeked'); }
      canvas.getContext('2d').drawImage(video, 0, 0, canvas.width, canvas.height);
      frames.push({...await frameFromCanvas(canvas), video:{seconds, time}});
    }
    return await finishVideo(file, frames, seconds, interval);
  } finally { video.removeAttribute('src'); video.load(); URL.revokeObjectURL(url); }
}

export async function frameFromCanvas(canvas) {
  const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/webp', 0.8));
  if (!blob || blob.type !== 'image/webp') throw new Error('Браузер не поддерживает WebP');
  const data = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result.split(',')[1]);
    reader.onerror = reject;
    reader.readAsDataURL(blob);
  });
  return {type:'image/webp', data};
}

export async function finishVideo(file, frames, seconds, interval) {
    let wav;
    try { wav = await wavFromRecording([file]); } catch (error) {
      if (error.name !== 'EncodingError') throw error;
    }
    // Кадры полезны и без слов: музыка, тишина или сбой распознавания не мешают отправить видео.
    let transcript = '';
    if (wav) {
      const response = await fetch('/voice', {method:'POST', headers:{'Content-Type':'audio/wav'}, body:wav});
      transcript = response.ok ? (await response.json()).text : `не удалась (${await response.text()})`;
    }
    return {frames, file, description:`[Видео ${duration(seconds)}. Кадры по порядку: ${frames.map((_, i) => `${i + 1} — ${duration(i * interval)}`).join(', ')}]${transcript ? `\nРасшифровка звука: ${transcript}` : ''}`};
}

// Описание кадров нужно агенту; в чате показываем текст Owner и расшифровку отдельно.
export function videoText(text) {
  const [own, transcript] = text.replace(/\[Видео [^\]\n]*\]\n?/, '').split(/\n*Расшифровка звука: /);
  return {text: own.trim(), transcript};
}
