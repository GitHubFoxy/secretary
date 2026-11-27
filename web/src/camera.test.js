import { expect, test } from 'bun:test';
import { createCamera } from './camera.js';

test('камера сообщает состояние после переподключения и отвечает на запрос кадра', async () => {
  const commands = [];
  let connected = true;
  const camera = createCamera({connected:() => connected, request:async command => { commands.push(command); }});
  camera.set(() => 'frame');
  expect(commands.at(-1)).toEqual({type:'cameraState', enabled:true});
  expect(camera.receive({type:'camera_frame', id:'one'})).toBe(true);
  expect(commands.at(-1)).toMatchObject({type:'cameraReply', id:'one', image:'frame'});
  connected = false; camera.set(null); expect(commands).toHaveLength(2);
  connected = true; camera.reconnect(); expect(commands.at(-1)).toEqual({type:'cameraState', enabled:false});
  camera.receive({type:'camera_frame', id:'two'});
  expect(commands.at(-1)).toMatchObject({type:'cameraReply', id:'two', error:'Камера не включена'});
  camera.set(() => 'again'); camera.reconnect();
  expect(commands.at(-1)).toEqual({type:'cameraState', enabled:true});
});
