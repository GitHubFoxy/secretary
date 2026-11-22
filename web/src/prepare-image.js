export function prepareImage(file) {
  return new Promise((resolve, reject) => {
    let worker;
    try { worker = new Worker(new URL('./prepare-image-worker.js', import.meta.url), {type:'module'}); }
    catch { reject(new Error('Не удалось обработать изображение. Выберите другой формат.')); return; }
    worker.onmessage = ({data}) => {
      worker.terminate();
      if (data.error) reject(new Error(data.error));
      else resolve(data);
    };
    worker.onerror = () => {
      worker.terminate();
      reject(new Error('Не удалось обработать изображение. Выберите другой формат.'));
    };
    worker.postMessage(file);
  });
}
