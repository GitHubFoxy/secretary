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
  let uploadInFlight = false;
  // Capture both Svelte submit and Enter before a reference exists. Once the
  // HTTP upload is acknowledged, sending during conversion is safe: Worker waits.
  form.addEventListener?.('submit', (event) => {
    if (!uploadInFlight) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    status.hidden = false;
    status.textContent = 'File is still uploading; send once its Artifact reference appears.';
  }, true);
  composer.addEventListener?.('keydown', (event) => {
    if (!uploadInFlight || event.key !== 'Enter' || event.shiftKey) return;
    event.preventDefault();
    event.stopImmediatePropagation();
  }, true);
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
    uploadInFlight = true;
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
      if (!ref || !result.artifact?.id) {
        // Legacy image upload responses also include artifact; reject malformed replies.
        throw new Error('Artifact response has no durable ID');
      }
      const text = composer.value.trimEnd();
      const prefix = imageFile(file) ? 'Analyze attached image ' : videoFile(file) ? 'Analyze sampled frames of attached video ' : 'Use attached file ';
      if (!composer.value.includes(ref)) {
        composer.value = text + (text ? '\n\n' : '') + prefix + ref + ' (' + file.name + ').';
        composer.dispatchEvent(new Event('input', { bubbles: true }));
      }
      uploadInFlight = false; // the canonical reference is already in the message
      composer.focus();

      if (videoFile(file)) {
        status.textContent = 'Video attached (' + ref + '). Processing in the background; you can send now.';
        // Re-selecting the failed file is an explicit retry of that original,
        // not a new upload; the same idempotency key remains in sessionStorage.
        if (result.duplicate && result.artifact.processing_status === 'failed') {
          const retry = await fetch('/v1/artifacts/' + encodeURIComponent(result.artifact.id) + '/retry', {
            method: 'POST', credentials: 'same-origin',
            headers: { 'Idempotency-Key': crypto.randomUUID() }
          });
          if (!retry.ok) throw new Error('Could not retry video: ' + (await retry.text()).trim());
          status.textContent = 'Retrying video ' + file.name + ' (' + ref + ')...';
        }
        const deadline = Date.now() + 125000;
        while (true) {
          const check = await fetch('/v1/artifacts/' + encodeURIComponent(result.artifact.id) + '/status', { credentials: 'same-origin' });
          if (!check.ok) throw new Error('Could not read video processing status: HTTP ' + check.status);
          const metadata = await check.json();
          if (metadata.processing_status === 'failed') throw new Error('Video processing failed: ' + (metadata.processing_error || 'unknown error') + '. Original retained: ' + ref + '. Select the same file to retry.');
          if (metadata.processing_status === 'ready' && metadata.video_frames?.length) break;
          if (Date.now() > deadline) throw new Error('Video still processing; original retained: ' + ref + '. You can send a question; Worker waits for conversion.');
          await new Promise(resolve => setTimeout(resolve, 500));
        }
      }
      try { sessionStorage.removeItem(pendingUpload.storageKey); } catch {}
      pendingUpload = null;
      status.textContent = 'Ready: ' + file.name + ' (' + ref + '). Send with a question. Videos are sampled: moments between frames are not covered.';
    } catch (error) {
      status.textContent = 'Upload failed: ' + error.message;
    } finally {
      uploadInFlight = false;
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
