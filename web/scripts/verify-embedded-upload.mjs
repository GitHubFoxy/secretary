// Validate the precise assets embedded and served by web/embed.go, not Vite's dev server.
// Run both before and after "npm run build:all": checked-in production HTML
// may explicitly load the module; the generated build must contain the uploader.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const read = (name) => readFileSync(join(root, name), 'utf8');
const html = read('index.html');
const app = read('app.js');
const uploader = read('artifact-upload.js');
const embed = read('embed.go');
const strictBundle = process.argv.includes('--require-bundled');

function assert(condition, detail) {
  if (!condition) throw new Error('production attachment check: ' + detail);
}

const declared = embed.match(/\/\/go:embed\s+([^\n]+)/)?.[1] || '';
for (const asset of ['index.html', 'app.js', 'artifact-upload.js']) {
  assert(declared.split(/\s+/).includes(asset), asset + ' is not in Go embedded file list');
  assert(read(asset).length > 0, asset + ' is empty');
}

const scriptTags = [...html.matchAll(/<script\b([^>]*)>/gi)].map((m) => m[1]);
const rawUploaderTags = scriptTags.filter((attrs) => /src\s*=\s*["']\/artifact-upload\.js["']/i.test(attrs));
for (const tag of rawUploaderTags) {
  assert(/type\s*=\s*["']module["']/i.test(tag), 'ES module loaded as classic script');
}
const directModule = rawUploaderTags.length > 0;
const appIsLoaded = scriptTags.some((attrs) =>
  /src\s*=\s*["']\/app\.js["']/i.test(attrs) && /type\s*=\s*["']module["']/i.test(attrs));
assert(appIsLoaded, 'Go embedded index.html does not load app.js as a module');

const expectedBundleMarkers = [
  'secretaryArtifactInput', '/v1/artifacts?name=', 'Idempotency-Key',
  'Attach image to Worker', 'Uploading '
];
const bundledUploader = expectedBundleMarkers.every((marker) => app.includes(marker));
assert(bundledUploader || (!strictBundle && directModule),
  'production Web has no working image attachment entrypoint');
if (strictBundle) {
  assert(bundledUploader, 'generated app.js lacks the uploader despite src/main.js import');
}
assert(uploader.includes('export function installUpload'),
  'standalone uploader is no longer a valid ES module');
assert(uploader.includes('new MutationObserver(installArtifactUpload)'),
  'production uploader does not attach to dynamically rendered composers');
console.log('Verified Go-embedded production attachment entrypoint; compiled uploader:', bundledUploader);
