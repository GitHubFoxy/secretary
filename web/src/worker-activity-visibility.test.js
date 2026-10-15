import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile, writeFile, mkdtemp, rm } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { compile } from 'svelte/compiler';
import { render } from 'svelte/server';

test('Worker observer can hide tool activity while retaining text and status', async () => {
  const directory = await mkdtemp(new URL('../.render-test-', import.meta.url));
  try {
    for (const name of ['Markdown', 'WorkerActivity']) {
      const source = await readFile(new URL(`./${name}.svelte`, import.meta.url), 'utf8');
      const code = compile(source, { generate: 'server' }).js.code
        .replaceAll("'./Markdown.svelte'", "'./Markdown.mjs'")
        .replaceAll("'./ui-model.js'", JSON.stringify(new URL('./ui-model.js', import.meta.url).href));
      await writeFile(`${directory}/${name}.mjs`, code);
    }
    const { default: Activity } = await import(pathToFileURL(`${directory}/WorkerActivity.mjs`));
    const events = [
      {id: 'tool-call', seq: 1, payload: {kind: 'tool_call', tool_call: {name: 'unknown.tool'}}},
      {id: 'tool-result', seq: 2, payload: {kind: 'tool_result', tool_result: {name: 'unknown.tool', status: 'failed', error: 'failed'}}},
      {id: 'text', seq: 3, payload: {kind: 'assistant_text_delta', text: 'visible text'}},
      {id: 'status', seq: 4, payload: {kind: 'status', status: 'working'}},
    ];
    const visible = (showToolActivity) => render(Activity, { props: { events, showToolActivity } }).body;
    const hidden = visible(false);
    assert.doesNotMatch(hidden, /unknown\.tool/);
    assert.match(hidden, /visible text/);
    assert.match(hidden, /Working/);
    assert.match(visible(true), /unknown\.tool/);
  } finally { await rm(directory, {recursive: true, force: true}); }
});
