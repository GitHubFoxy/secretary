import { readFileSync } from 'node:fs';
import { hostPath, mcpPort, secretaryFolder } from './config';
import { storeFile } from './files';
import { modelDetails } from './models';
import { specialistModel } from './persona';
import { isSpecialist, SECRETARY_ID } from './session';
import type { Sessions } from './sessions';

const issuesUrl = 'https://github.com/GitHubFoxy/secretary/issues/new';

// The owner sends the issue from their own GitHub account, so the Host needs no token.
export function issueUrl(title: string, body: string, caller: { harness: string; model: string }, build = buildVersion()) {
  const details = `\n\n---\nВерсия: ${build}\nHarness: ${caller.harness}, модель ${caller.model}\nСистема: ${process.platform} ${process.arch}`;
  return `${issuesUrl}?${new URLSearchParams({ title, body: body.slice(0, 6000) + details, labels: 'needs-triage' })}`;
}

function buildVersion() {
  try { return JSON.parse(readFileSync(new URL('../web/dist/build-version.json', import.meta.url), 'utf8')).version; } catch { return 'неизвестна'; }
}

type ModelList = (harness: string) => Promise<{ id: string; supportedReasoningEfforts: { reasoningEffort: string }[] }[]>;

// One MCP adapter for both Secretary Agent (level 0) and Specialists (level 1).
// The caller's session id is in the path (/mcp/<id>); level-2 child sessions do not get MCP.
export function serveMcp(sessions: Sessions, { port = mcpPort, listModels = modelDetails as ModelList, root = secretaryFolder } = {}) {
  function toolsFor(isSecretary: boolean) {
    const target = isSecretary ? 'Specialist' : 'Worker';
    const targetGen = isSecretary ? 'Specialists' : 'твоих Workers';
    return [
      [
        'list',
        `Показать список ${targetGen} (id, имя, папка, harness, модель, статус) и доступные на этом Host модели с уровнями reasoning. Скрытые владельцем — завершённые — показываются только с hidden: true.`,
        { hidden: { type: 'boolean', description: 'Показать только скрытые владельцем' } },
        [],
      ],
      [
        'send',
        isSecretary
          ? 'Отправить задачу Specialist. С id — пишет существующему (по умолчанию в очередь; immediate: true прерывает ход). Без id — создаёт нового Specialist по name и folder и сразу запускает задачу (harness, model, reasoning — из настройки владельца для новых Specialists, иначе как у тебя).'
          : 'Отправить задачу Worker. С id — пишет существующему (по умолчанию в очередь; immediate: true прерывает ход). Без id — создаёт нового Worker по name (folder необязателен, по умолчанию твоя папка; harness, model, reasoning наследуются).',
        {
          message: { type: 'string', description: 'Текст задачи или сообщения' },
          id: { type: 'number', description: `id существующего ${target}` },
          immediate: { type: 'boolean', description: 'Прервать текущий ход и отправить сразу (только с id)' },
          name: { type: 'string', minLength: 1, description: isSecretary ? 'Короткое имя Specialist по области ответственности (например, Заметки, [Название проекта], Исследователь, Общий)' : 'Короткое имя Worker по сути задачи' },
          folder: { type: 'string', description: isSecretary ? 'Рабочая папка (обязательно без id)' : 'Рабочая папка или путь к git worktree (по умолчанию твоя папка)' },
          harness: { type: 'string', enum: sessions.harnesses, description: 'Harness (по умолчанию — см. описание send)' },
          model: { type: 'string', description: 'Модель из list (по умолчанию — см. описание send)' },
          reasoning: { type: 'string', description: 'Уровень reasoning из list (по умолчанию — см. описание send)' },
        },
        ['message'],
      ],
      [
        'read',
        `Прочитать последние сообщения ${target} по id. По умолчанию возвращает последние 10 очищенных сообщений диалога (user, agent, report, error); передай raw: true для сырых событий.`,
        {
          id: { type: 'number', description: `id ${target}` },
          limit: { type: 'number', description: 'Сколько сообщений вернуть (1–100, по умолчанию 10)' },
          raw: { type: 'boolean', description: 'Вернуть сырые события harness вместо очищенных сообщений' },
        },
        ['id'],
      ],
      [
        'stop',
        isSecretary
          ? 'Немедленно остановить Specialist по id вместе с его Workers.'
          : 'Немедленно остановить одного из твоих Workers по id.',
        { id: { type: 'number', description: `id ${target}` } },
        ['id'],
      ],
      ...(isSecretary ? [[
        'send_file',
        'Показать владельцу файл с Host в чате: картинку, видео, аудио или документ. Владелец сможет открыть его и скачать на устройство.',
        {
          path: { type: 'string', minLength: 1, description: 'Путь к файлу на Host: абсолютный или от твоей папки' },
          caption: { type: 'string', description: 'Подпись к файлу' },
        },
        ['path'],
      ], [
        'report_issue',
        'Подготовить сообщение об ошибке или идее для разработчиков Secretary App. Возвращает ссылку на заполненный GitHub issue: отдай её владельцу, он проверит текст и отправит.',
        {
          title: { type: 'string', minLength: 1, description: 'Коротко, что случилось' },
          body: { type: 'string', minLength: 1, description: 'Что делал владелец, что ожидал, что произошло; текст ошибок, если есть' },
        },
        ['title', 'body'],
      ]] : []),
    ].map(([name, description, properties, required]) => ({ name, description, inputSchema: { type: 'object', properties, required } }));
  }

  async function availableModels() {
    const result: Record<string, { id: string; reasoning: string[] }[]> = {};
    await Promise.all(sessions.harnesses.map(async harness => {
      try {
        const list = await listModels(harness);
        result[harness] = list.map(m => ({ id: m.id, reasoning: m.supportedReasoningEfforts.map(e => e.reasoningEffort) }));
      } catch {}
    }));
    return result;
  }

  function resolveTarget(callerId: number, isSecretary: boolean, id: unknown, allowSelf = false) {
    const target = sessions.get(id);
    if (!allowSelf && target.id === callerId) throw new Error('Нельзя выполнить это действие для самой себя');
    if (target.id === SECRETARY_ID && !allowSelf) throw new Error('Нельзя выполнить это действие для Secretary Agent');
    if (!isSecretary && target.info.parent_id !== callerId) throw new Error('Нет доступа к чужой сессии');
    return target;
  }

  async function call(callerId: number, isSecretary: boolean, name: string, args: any) {
    switch (name) {
      case 'list': {
        const all = sessions.list().filter(s => (isSecretary ? isSpecialist(s) : s.parent_id === callerId) && !!s.hidden === (args.hidden === true));
        return { sessions: all, models: await availableModels() };
      }
      case 'send': {
        if (typeof args.message !== 'string' || !args.message.trim()) throw new Error('Пустое сообщение');
        if (args.id !== undefined) {
          const target = resolveTarget(callerId, isSecretary, args.id);
          target.send(args.message, undefined, args.immediate === true);
          return { id: target.id, status: target.info.status };
        }
        if (typeof args.name !== 'string' || !args.name.trim()) throw new Error('Укажите name для новой сессии');
        const caller = sessions.get(callerId).info;
        const folder = typeof args.folder === 'string' && args.folder.trim()
          ? args.folder.trim()
          : (isSecretary ? '' : caller.folder);
        if (!folder) throw new Error('Укажите folder для нового Specialist');
        const base = (isSecretary && specialistModel(root)) || caller;
        const harness = typeof args.harness === 'string' && args.harness.trim() ? args.harness.trim() : base.harness;
        const sameHarness = harness === base.harness;
        const model = typeof args.model === 'string' && args.model.trim()
          ? args.model.trim()
          : (sameHarness ? base.model : '');
        if (!model) throw new Error('При смене harness укажите model из list');
        const reasoning = typeof args.reasoning === 'string' && args.reasoning.trim()
          ? args.reasoning.trim()
          : (sameHarness && model === base.model ? base.reasoning : null);
        const child = await sessions.create({ harness, model, reasoning, folder, parentId: callerId, name: args.name.trim() });
        child.send(args.message);
        return { id: child.id, status: 'running' };
      }
      case 'read': {
        const target = resolveTarget(callerId, isSecretary, args.id, isSecretary);
        const limit = Math.max(1, Math.min(100, Number(args.limit) || 10));
        if (args.raw === true) {
          return target.events().slice(-limit).map(event => Array.isArray(event.images) ? { ...event, images: event.images.length } : event);
        }
        return sessions.chatHistory(target.id, limit).map(m => 'images' in m ? { ...m, images: m.images.length } : m);
      }
      case 'stop': {
        const target = resolveTarget(callerId, isSecretary, args.id);
        sessions.stop(target.id);
        return { ok: true };
      }
      case 'send_file': {
        if (!isSecretary) throw new Error('Неизвестный инструмент');
        if (typeof args.path !== 'string' || !args.path.trim()) throw new Error('Укажите path');
        const caller = sessions.get(callerId);
        const file = await storeFile(hostPath(args.path.trim(), caller.info.folder), root);
        caller.showFiles([file], typeof args.caption === 'string' ? args.caption.trim() : '');
        return { name: file.name, size: file.size };
      }
      case 'report_issue': {
        if (!isSecretary) throw new Error('Неизвестный инструмент');
        if (typeof args.title !== 'string' || !args.title.trim() || typeof args.body !== 'string' || !args.body.trim()) throw new Error('Укажите title и body');
        return { url: issueUrl(args.title.trim(), args.body.trim(), sessions.get(callerId).info) };
      }
      default:
        throw new Error('Неизвестный инструмент');
    }
  }

  return Bun.serve({
    hostname: '127.0.0.1',
    port,
    async fetch(req) {
      const path = new URL(req.url).pathname;
      const match = /^\/mcp(?:\/(\d+))?$/.exec(path);
      if (!match || req.method !== 'POST') return new Response(null, { status: 405 });
      const callerId = match[1] ? Number(match[1]) : SECRETARY_ID;
      let caller;
      try { caller = sessions.get(callerId).info; } catch { return new Response('Session not found', { status: 404 }); }
      const isSecretary = caller.id === SECRETARY_ID;
      if (!isSecretary && !isSpecialist(caller)) return new Response('Forbidden', { status: 403 });

      const msg = await req.json();
      if (msg.id === undefined) return new Response(null, { status: 202 });
      const ok = (result: unknown) => Response.json({ jsonrpc: '2.0', id: msg.id, result });
      if (msg.method === 'initialize') return ok({ protocolVersion: msg.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'secretary', version: '0.1' } });
      if (msg.method === 'tools/list') return ok({ tools: toolsFor(isSecretary) });
      if (msg.method === 'tools/call') {
        try { return ok({ content: [{ type: 'text', text: JSON.stringify(await call(callerId, isSecretary, msg.params.name, msg.params.arguments || {})) }] }); }
        catch (error) { return ok({ isError: true, content: [{ type: 'text', text: String(error) }] }); }
      }
      return Response.json({ jsonrpc: '2.0', id: msg.id, error: { code: -32601, message: 'Method not found' } });
    },
  });
}
