import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile, writeFile, mkdtemp, rm } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { compile } from 'svelte/compiler';
import { render } from 'svelte/server';

test('chat renders readable Markdown and code while keeping HTML and unsafe links inert', async () => {
  const directory = await mkdtemp(new URL('../.render-test-', import.meta.url));
  try {
    const source = await readFile(new URL('./Markdown.svelte', import.meta.url), 'utf8');
    const compiled = compile(source, { generate: 'server' });
    const file = `${directory}/component.mjs`;
    await writeFile(file, compiled.js.code);
    const { default: Markdown } = await import(pathToFileURL(file));
    const { body } = render(Markdown, { props: { text: '# Ответ\n\n**готово** [документ](https://example.com)\n\n```js\nconst x = "<script>";\n```\n\n<img src=x onerror=alert(1)> [опасно](javascript:alert(1))' } });
    assert.match(body, /<h1>Ответ<\/h1>/);
    assert.match(body, /<strong>готово<\/strong>/);
    assert.match(body, /href="https:\/\/example.com"/);
    assert.match(body, /<pre><code class="language-js">const x = &quot;&lt;script&gt;&quot;;/);
    assert.doesNotMatch(body, /<img|href="javascript:|<script>/);
    assert.match(body, /&lt;img src=x onerror=alert\(1\)&gt;/);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test('Secretary stream joins Markdown chunks and removes reply text by canonical turn identity or terminal', async () => {
  const directory = await mkdtemp(new URL('../.render-test-', import.meta.url));
  try {
    for (const name of ['Markdown', 'SecretaryStream']) {
      const source = await readFile(new URL(`./${name}.svelte`, import.meta.url), 'utf8');
      const code = compile(source, { generate: 'server' }).js.code
        .replaceAll("'./Markdown.svelte'", "'./Markdown.mjs'")
        .replaceAll("'./ui-model.js'", JSON.stringify(new URL('./ui-model.js', import.meta.url).href));
      await writeFile(`${directory}/${name}.mjs`, code);
    }
    const { default: Stream } = await import(pathToFileURL(`${directory}/SecretaryStream.mjs`));
    const events = [{id: 'e1', kind: 'secretary.text_delta', payload: {text: '**гото'}}, {id: 'e2', kind: 'secretary.text_delta', payload: {text: 'во**'}}];
    const visible = (props) => render(Stream, {props: {events, turnId: 'turn-1', ...props}}).body;
    assert.match(visible({}), /<strong>готово<\/strong>/);
    assert.match(visible({entries: [{kind: 'secretary', turn_id: 'another', body: '**готово**'}]}), /<strong>готово<\/strong>/);
    assert.doesNotMatch(visible({entries: [{kind: 'secretary', turn_id: 'turn-1', body: '**готово**'}]}), /<strong>готово<\/strong>/);
    const finished = [...events, {id: 'terminal', kind: 'secretary.turn.finished', payload: {status: 'succeeded'}}];
    assert.match(visible({events: finished}), /не содержит сохранённого ответа/);
    assert.doesNotMatch(visible({events: finished}), /<strong>готово<\/strong>/);
    assert.doesNotMatch(visible({events: [...events, {id: 'terminal', kind: 'secretary.turn.finished', payload: {status: 'succeeded', conversation_entry_id: 'ent-1'}}]}), /не содержит сохранённого ответа/);
    assert.match(visible({events: [{kind: 'secretary.turn.finished', payload: {status: 'failed', error: 'native runtime failed'}}]}), /role="alert"[^>]*>native runtime failed/);
  } finally { await rm(directory, {recursive: true, force: true}); }
});

test('Worker queue shows durable message state and delivery failures', async () => {
  const directory = await mkdtemp(new URL('../.render-test-', import.meta.url));
  try {
    for (const name of ['Markdown', 'WorkerMessages']) {
      const source = await readFile(new URL(`./${name}.svelte`, import.meta.url), 'utf8');
      await writeFile(`${directory}/${name}.mjs`, compile(source, {generate: 'server'}).js.code.replaceAll("'./Markdown.svelte'", "'./Markdown.mjs'"));
    }
    const {default: Queue} = await import(pathToFileURL(`${directory}/WorkerMessages.mjs`));
    const {body} = render(Queue, {props: {messages: [
      {id: 'q1', sequence: 1, state: 'pending', text: '**Проверь тесты**'},
      {id: 'q2', sequence: 2, state: 'blocked', text: 'Продолжи', last_error: 'Node offline'},
    ]}});
    assert.match(body, /Queued · #1/);
    assert.match(body, /<strong>Проверь тесты<\/strong>/);
    assert.match(body, /Blocked · #2/);
    assert.match(body, /role="alert"[^>]*>Node offline/);
  } finally { await rm(directory, {recursive: true, force: true}); }
});
