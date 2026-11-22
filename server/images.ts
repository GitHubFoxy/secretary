export type Image = { type: 'image/png' | 'image/jpeg' | 'image/gif' | 'image/webp'; data: string; video?: { seconds: number; time: number; note?: boolean } };

export function validateImages(value: unknown): Image[] {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.length > 63) throw new Error('Можно прикрепить не более 63 изображений');
  return value.map(image => {
    if (!image || typeof image.data !== 'string' || image.data.length > 7_000_000 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(image.data)) throw new Error('Некорректное или слишком большое изображение (максимум 5 МБ)');
    const b = Buffer.from(image.data, 'base64');
    if (b.length > 5 * 1024 * 1024) throw new Error('Изображение превышает 5 МБ');
    const type = b.subarray(0, 8).equals(Buffer.from([137,80,78,71,13,10,26,10])) ? 'image/png' : b.subarray(0, 3).equals(Buffer.from([255,216,255])) ? 'image/jpeg' : ['GIF87a','GIF89a'].includes(b.subarray(0, 6).toString()) ? 'image/gif' : b.subarray(0, 4).toString() === 'RIFF' && b.subarray(8, 12).toString() === 'WEBP' ? 'image/webp' : null;
    if (!type || image.type !== type) throw new Error('Поддерживаются только PNG, JPEG, GIF и WebP');
    if (image.video !== undefined && (!image.video || !Number.isFinite(image.video.seconds) || image.video.seconds <= 0 || image.video.seconds > 60 || !Number.isInteger(image.video.time) || image.video.time < 0 || image.video.time >= image.video.seconds)) throw new Error('Некорректное время кадра');
    if (image.video?.note !== undefined && typeof image.video.note !== 'boolean') throw new Error('Некорректный признак видеосообщения');
    return { type, data: image.data, ...(image.video ? {video:{seconds:image.video.seconds, time:image.video.time, ...(image.video.note ? {note:true} : {})}} : {}) } as Image;
  });
}
