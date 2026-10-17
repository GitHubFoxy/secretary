function installArtifactUpload() {
  const composer = document.querySelector('textarea#message');
  const form = composer?.closest('form');
  if (!form || form.querySelector('[data-secretary-artifact-input]')) return;

  const label = document.createElement('label');
  label.textContent = 'Attach';
  label.setAttribute('aria-label', 'Attach a document (up to 8 MiB)');
  label.style.cssText = 'cursor:pointer;border:1px solid #475569;border-radius:12px;padding:12px;color:#cbd5e1;font-size:13px;white-space:nowrap';
  const input = document.createElement('input');
  input.type = 'file';
  input.hidden = true;
  input.dataset.secretaryArtifactInput = '1';
  input.accept = '.txt,.md,.pdf,.csv,.json,.xml,.yaml,.yml,.html,.log,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.odt,.ods,.rtf,.zip,.png,.jpg,.jpeg,.webp';
  label.append(input);
  const status = document.createElement('span');
  status.setAttribute('role', 'status');
  status.setAttribute('aria-live', 'polite');
  status.style.cssText = 'position:absolute;bottom:100%;left:0;font-size:12px;max-width:100%;padding:4px 8px;border-radius:8px;background:#111827;color:#cbd5e1;word-break:break-word';
  status.hidden = true;
  form.style.position = 'relative';
  form.insertBefore(label, composer);
  form.append(status);

  input.addEventListener('change', async () => {
    const file = input.files?.[0];
    if (!file) return;
    status.hidden = false;
    if (file.size === 0 || file.size > 8 * 1024 * 1024) {
      status.textContent = 'Upload rejected: file must be 1 byte to 8 MiB.';
      input.value = '';
      return;
    }
    status.textContent = 'Uploading ' + file.name + '...';
    input.disabled = true;
    try {
      const response = await fetch('/v1/artifacts?name=' + encodeURIComponent(file.name), {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Idempotency-Key': crypto.randomUUID(), 'Content-Type': 'application/octet-stream' },
        body: file
      });
      if (!response.ok) throw new Error((await response.text()).trim() || 'Upload failed');
      const result = await response.json();
      const ref = result.reference;
      status.textContent = 'Ready: ' + file.name + ' (' + ref + '). Send a message to use it.';
      const text = composer.value.trimEnd();
      composer.value = text + (text ? '\n\n' : '') + 'Use attached file ' + ref + ' (' + file.name + ').';
      composer.dispatchEvent(new Event('input', { bubbles: true }));
      composer.focus();
    } catch (error) {
      status.textContent = 'Upload failed: ' + error.message;
    } finally {
      input.disabled = false;
      input.value = '';
    }
  });
}

if (typeof document !== 'undefined') {
  const observer = new MutationObserver(installArtifactUpload);
  observer.observe(document.documentElement, { childList: true, subtree: true });
  installArtifactUpload();
}
