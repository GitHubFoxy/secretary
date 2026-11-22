onmessage = async ({data:file}) => {
  let bitmap;
  try {
    bitmap = await createImageBitmap(file, {imageOrientation:'from-image'});
    const scale = Math.min(1, 2048 / Math.max(bitmap.width, bitmap.height));
    const canvas = new OffscreenCanvas(Math.max(1, Math.round(bitmap.width * scale)), Math.max(1, Math.round(bitmap.height * scale)));
    canvas.getContext('2d').drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await canvas.convertToBlob({type:'image/webp', quality:0.8});
    if (blob.type !== 'image/webp') throw new Error();
    if (blob.size > 5 * 1024 * 1024) { postMessage({error:'После сжатия изображение превышает 5 МБ'}); return; }
    const data = await new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result.split(',')[1]);
      reader.onerror = reject;
      reader.readAsDataURL(blob);
    });
    postMessage({type:'image/webp', data});
  } catch (error) {
    postMessage({error:'Не удалось обработать изображение. Выберите другой формат.'});
  } finally { bitmap?.close(); }
};
