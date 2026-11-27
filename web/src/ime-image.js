export async function receiveImeImage(data, {fetchFile, isFocused, add, done}) {
  let item;
  try {
    item = JSON.parse(data);
    if (!item.id || typeof item.session !== 'string' || !item.type?.startsWith('image/')) throw new Error('Неверное изображение');
    const response = await fetchFile(item.id);
    if (!response.ok) throw new Error('Не удалось прочитать изображение');
    const blob = await response.blob();
    if (isFocused(item.session)) add(new File([blob], 'clipboard-image', {type:item.type}));
  } finally {
    if (item?.id) done(item.id);
  }
}
