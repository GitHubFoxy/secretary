export function clipboardImages(data) {
  const files = Array.from(data?.files || []).filter(file => file.type.startsWith('image/'));
  if (files.length) return files;
  return Array.from(data?.items || [])
    .filter(item => item.kind === 'file' && item.type.startsWith('image/'))
    .map(item => item.getAsFile())
    .filter(Boolean);
}
