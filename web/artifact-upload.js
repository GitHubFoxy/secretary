const artifactFormats = '.txt,.md,.pdf,.csv,.json,.xml,.yaml,.yml,.html,.log,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.odt,.ods,.rtf,.zip,.png,.jpg,.jpeg,.webp,.mp4,.webm,.mov';
const imageFormats = '.png,.jpg,.jpeg,.webp,.mp4,.webm,.mov';
const videoFile = (file) => /\.(mp4|webm|mov)$/i.test(file.name);
const imageFile = (file) => /\.(png|jpe?g|webp)$/i.test(file.name);

async function uploadFingerprint(file) {
  const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
  return file.name + ':' + file.size + ':' + Array.from(new Uint8Array(digest), value => value.toString(16).padStart(2, '0')).join('');
}

export function installUpload(composer, imageOnly) {
  const form = composer.closest('form');
  if (!form || form.querySelector('[data-secretary-artifact-input]')) return;

  const label = document.createElement('label');
  label.textContent = 'Attach';
  label.setAttribute('aria-label', imageOnly ? 'Attach image or video to Worker' : 'Attach document, image or video (up to 8 MiB)');
  label.style.cssText = 'cursor:pointer;border:1px solid #475569;border-radius:12px;padding:10px;color:#cbd5e1;font-size:12px;white-space:nowrap';
  const input = document.createElement('input');
  input.type = 'file';
  input.hidden = true;
  input.dataset.secretaryArtifactInput = '1';
  input.accept = imageOnly ? imageFormats : artifactFormats;
  label.append(input);
  const status = document.createElement('span');
  status.setAttribute('role', 'status');
  status.setAttribute('aria-live', 'polite');
  status.style.cssText = 'position:absolute;bottom:100%;left:0;font-size:12px;max-width:100%;padding:4px 8px;border-radius:8px;background:#111827;color:#cbd5e1;word-break:break-word';
  status.hidden = true;
  form.style.position = 'relative';
  form.insertBefore(label, composer);
  form.append(status);

  let pendingUpload = null;
  input.addEventListener('change', async () => {
    const file = input.files?.[0];
    if (!file) return;
    status.hidden = false;
    if (file.size === 0 || file.size > 8 * 1024 * 1024) {
      status.textContent = 'Upload rejected: file must be 1 byte to 8 MiB.';
      input.value = '';
      return;
    }
    if (imageOnly && !imageFile(file) && !videoFile(file)) {
      status.textContent = 'Worker vision input supports PNG, JPEG, WebP, MP4, WebM and MOV.';
      input.value = '';
      return;
    }
    status.textContent = 'Uploading ' + file.name + '...';
    input.disabled = true;
    try {
      const fingerprint = await uploadFingerprint(file);
      const storageKey = 'secretary-upload:' + fingerprint;
      if (!pendingUpload || pendingUpload.fingerprint !== fingerprint) {
        let previousKey = '';
        try { previousKey = sessionStorage.getItem(storageKey) || ''; } catch {}
        pendingUpload = { fingerprint, storageKey, key: previousKey || crypto.randomUUID() };
        try { sessionStorage.setItem(storageKey, pendingUpload.key); } catch {}
      }
      const response = await fetch('/v1/artifacts?name=' + encodeURIComponent(file.name), {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Idempotency-Key': pendingUpload.key, 'Content-Type': 'application/octet-stream' },
        body: file
      });
      if (!response.ok) throw new Error((await response.text()).trim() || 'Upload failed');
      const result = await response.json();

      const ref = result.reference;
      if (videoFile(file)) {
        status.textContent = 'Processing video ' + file.name + ' (you can continue other chats)...';
        const deadline = Date.now() + 125000;
        while (true) {
          const check = await fetch('/v1/artifacts/' + encodeURIComponent(result.artifact.id), { credentials: 'same-origin' });
          if (!check.ok) throw new Error('Could not read video processing status: HTTP ' + check.status);
          const metadata = await check.json();
          if (metadata.processing_status === 'failed') throw new Error('Video processing failed: ' + (metadata.processing_error || 'unknown error') + '. Original retained: ' + ref);
          if (metadata.processing_status === 'ready' && metadata.video_frames?.length) break;
          if (Date.now() > deadline) throw new Error('Video still processing; original retained: ' + ref + '. Check status before sending.');
          await new Promise(resolve => setTimeout(resolve, 500));
        }
      }
      try { sessionStorage.removeItem(pendingUpload.storageKey); } catch {}
      pendingUpload = null;
      status.textContent = 'Ready: ' + file.name + ' (' + ref + '). Send with a question. Videos are sampled: moments between frames are not covered.';
      const text = composer.value.trimEnd();
      const prefix = imageFile(file) ? 'Analyze attached image ' : videoFile(file) ? 'Analyze sampled frames of attached video ' : 'Use attached file ';
      composer.value = text + (text ? '\n\n' : '') + prefix + ref + ' (' + file.name + ').';
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

function installArtifactUpload() {
  for (const composer of document.querySelectorAll('textarea#message')) installUpload(composer, false);
  for (const composer of document.querySelectorAll('input[placeholder="Message Worker · /q to queue"], input[placeholder="Follow-up for this Worker"]')) installUpload(composer, true);
}

if (typeof document !== 'undefined') {
  const observer = new MutationObserver(installArtifactUpload);
  observer.observe(document.documentElement, { childList: true, subtree: true });
  installArtifactUpload();
}
