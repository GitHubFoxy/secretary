import { expect, test } from 'bun:test';
import { Speech } from './speech';

test('озвучивает только текст агента по предложениям в порядке сообщений и останавливает очередь', async () => {
  const spoken: string[] = [], sent: any[] = [];
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const speech = new Speech(message => sent.push(message), async (text, signal, send) => {
    spoken.push(text);
    if (text === 'Первая фраза.') await gate;
    if (!signal.aborted) send(text, 24000);
  });
  speech.start();
  speech.event({type:'assistant', message:{content:[{type:'text', text:'**Первая фраза.** Вторая фраза!'}, {type:'tool_use', name:'run', input:{command:'секрет'}}]}});
  speech.event({method:'item/completed', params:{item:{type:'commandExecution', text:'результат'}}});
  speech.event({method:'item/completed', params:{item:{type:'agentMessage', text:'Третья фраза.'}}});
  await Bun.sleep(0);
  expect(spoken).toEqual(['Первая фраза.']);
  release();
  speech.end(false);
  await Bun.sleep(0);
  expect(spoken).toEqual(['Первая фраза.', 'Вторая фраза!', 'Третья фраза.']);
  expect(sent.map(item => item.type)).toEqual(['speech_audio', 'speech_audio', 'speech_audio', 'speech_done']);

  speech.start();
  speech.event({type:'assistant', message:{content:[{type:'text', text:'Не озвучивать.'}]}});
  speech.stop();
  await Bun.sleep(0);
  expect(spoken).not.toContain('Не озвучивать.');
});
