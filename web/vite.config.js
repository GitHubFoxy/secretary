import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { randomUUID } from 'node:crypto';

const buildId = randomUUID();
export default defineConfig({
  define: { __SECRETARY_BUILD_ID__: JSON.stringify(buildId) },
  plugins: [svelte(), tailwindcss(), {
    name: 'secretary-build-version',
    generateBundle() { this.emitFile({type:'asset', fileName:'build-version.json', source:JSON.stringify({version:buildId})}); },
  }],
});
