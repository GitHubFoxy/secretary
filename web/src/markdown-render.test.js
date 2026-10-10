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

test('Worker activity joins native text fragments while preserving attempt and tool boundaries', async () => {
  const directory = await mkdtemp(new URL('../.render-test-', import.meta.url));
  try {
    for (const name of ['Markdown', 'WorkerActivity']) {
      const source = await readFile(new URL(`./${name}.svelte`, import.meta.url), 'utf8');
      const code = compile(source, {generate: 'server'}).js.code
        .replaceAll("'./Markdown.svelte'", "'./Markdown.mjs'")
        .replaceAll("'./ui-model.js'", JSON.stringify(new URL('./ui-model.js', import.meta.url).href));
      await writeFile(`${directory}/${name}.mjs`, code);
    }
    const {default: Activity} = await import(pathToFileURL(`${directory}/WorkerActivity.mjs`));
    let seq = 0;
    const event = (kind, text, attempt = 'attempt-1', extra = {}) => ({
      id: `event-${++seq}`, seq, kind: 'attempt.activity', attempt_id: attempt,
      payload: {metadata: {worker_ref: 'worker-1', attempt_id: attempt}, kind, ...(text ? {text} : {}), ...extra},
    });
    const markdown = '# Проверка\n\n**готово**\n\n```javascript\nconsole.log("ok");\n```\n' + 'Полный текст. '.repeat(30);
    const text = [...markdown].map((chunk) => event('assistant_text_delta', chunk));
    const events = [...text, text[0],
      event('tool_call', '', 'attempt-1', {tool_call: {name: 'mcp.read', preview: 'marker', arguments: 'private arguments'}}),
      event('assistant_text_delta', 'После инструмента'),
      event('status', '', 'attempt-1', {status: 'working'}),
      event('assistant_text_delta', 'Новая попытка', 'attempt-2'),
      event('thinking', 'private reasoning', 'attempt-2'),
      event('assistant_text_delta', 'не склеивать', 'attempt-2'),
      event('assistant_text_delta', 'analysis secret', 'attempt-2', {channel: 'analysis'}),
      event('assistant_text_delta', 'Другой канал', 'attempt-2', {channel: 'final'}),
      event('assistant_text_delta', 'Ещё канал', 'attempt-2', {channel: 'commentary'}),
      event('assistant_text_delta', 'Следующая попытка', 'attempt-3', {channel: 'commentary'}),
      event('user_input_request', '', 'attempt-3', {request: {request_id: 'input-1', prompt: 'Нужен ответ'}}),
      event('assistant_text_delta', 'После ввода', 'attempt-3', {channel: 'commentary'}),
    ];
    const {body} = render(Activity, {props: {events}});
    assert.equal((body.match(/aria-label="Worker activity item"/g) || []).length, 11);
    assert.match(body, /<h1>Проверка<\/h1>/);
    assert.match(body, /<strong>готово<\/strong>/);
    assert.match(body, /Полный текст\. Полный текст\./);
    assert.equal((body.match(/Полный текст\./g) || []).length, 30);
    assert.match(body, /<pre><code class="language-javascript">console.log\(&quot;ok&quot;\);/);
    assert.match(body, /▶ mcp.read marker/);
    assert.match(body, /После инструмента/);
    assert.match(body, /Working/);
    assert.doesNotMatch(body, /private reasoning|analysis secret|private arguments|attempt.activity/);
    assert.ok(body.indexOf('▶ mcp.read marker') < body.indexOf('После инструмента'));
  } finally { await rm(directory, {recursive: true, force: true}); }
});
