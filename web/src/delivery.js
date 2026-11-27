export function createDelivery({request, fetch, onChange, revoke}) {
  const deliveries = new Map();
  let preparing = [];
  function update(items) { preparing = items; onChange(items); }
  async function send(sent) {
    const previous = deliveries.get(sent.sessionId) ?? Promise.resolve();
    let finish;
    const delivery = new Promise(resolve => { finish = resolve; });
    deliveries.set(sent.sessionId, delivery);
    update([...preparing, sent]);
    try {
      const results = await Promise.all(sent.images.map(image => image.ready));
      sent.images = sent.images.map((image, index) => ({...image, processing:false, error:results[index].error}));
      const failed = results.find(result => result.error);
      if (failed) throw new Error(failed.error);
      await previous;
      let offset = 0;
      const descriptions = results.map(result => {
        const start = offset; offset += result.frames.length;
        return result.description?.replace('Кадры по порядку:', `Кадры по порядку, начиная с изображения ${start + 1}:`);
      }).filter(Boolean);
      const uploaded = await Promise.all(results.map(async result => {
        if (!result.file) return null;
        const response = await fetch(`/files?name=${encodeURIComponent(result.file.name)}`, {method:'POST', headers:{'Content-Type':result.file.type || 'application/octet-stream'}, body:result.file});
        if (!response.ok) throw new Error(await response.text());
        return response.json();
      }));
      const files = uploaded.filter(Boolean);
      const frames = results.flatMap((result, index) => result.frames.map(frame => uploaded[index] ? {...frame, originalId:uploaded[index].id} : frame));
      if (JSON.stringify(frames).length > 29 * 1024 * 1024) throw new Error('Вложения слишком большие для отправки');
      await request({type:'send', id:sent.sessionId, text:[sent.text, ...descriptions].filter(Boolean).join('\n\n'), images:frames, files, immediate:sent.immediate});
      update(preparing.filter(item => item.id !== sent.id));
      sent.images.forEach(image => { if (image.url) revoke(image.url); });
    } catch (err) {
      const detail = err.disconnected ? 'Не удалось подтвердить доставку. Проверьте историю перед повторной отправкой.' : err.message;
      update(preparing.map(item => item.id === sent.id ? {...item, error:detail} : item));
    } finally {
      await previous;
      finish();
      if (deliveries.get(sent.sessionId) === delivery) deliveries.delete(sent.sessionId);
    }
  }
  function restore(id) {
    const item = preparing.find(entry => entry.id === id);
    if (item) update(preparing.filter(entry => entry.id !== id));
    return item;
  }
  return {send, restore};
}
