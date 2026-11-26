export async function isTurnComplete(wav, send = fetch) {
  try {
    const response = await send('/voice/turn', {method:'POST', headers:{'Content-Type':'audio/wav'}, body:wav, signal:AbortSignal.timeout(2000)});
    if (!response.ok) return true;
    return (await response.json()).complete !== false;
  } catch { return true; }
}
