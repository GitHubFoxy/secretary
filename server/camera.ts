import { filePath, saveFile } from './files';
import { validateImages } from './images';

type Client = (message: object) => void;
export function cameraRequests(root: string, timeout = 15000) {
  const clients = new Set<Client>();
  const pending = new Map<string, {client: Client; resolve: (path: string) => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout>}>();
  function remove(client: Client) {
    clients.delete(client);
    for (const [id, request] of pending) if (request.client === client) {
      clearTimeout(request.timer); pending.delete(id); request.reject(new Error('Камера не включена'));
    }
  }
  return {
    set(client: Client, enabled: boolean) { if (enabled) { clients.delete(client); clients.add(client); } else remove(client); },
    remove,
    request() {
      const client = Array.from(clients).at(-1);
      if (!client) throw new Error('Камера не включена');
      const id = crypto.randomUUID();
      return new Promise<string>((resolve, reject) => {
        const timer = setTimeout(() => { pending.delete(id); reject(new Error('Время ожидания камеры истекло')); }, timeout);
        pending.set(id, {client, resolve, reject, timer});
        client({type:'camera_frame', id});
      });
    },
    async reply(client: Client, id: string, image: unknown, error: unknown) {
      const request = pending.get(id);
      if (!request || request.client !== client) return;
      pending.delete(id); clearTimeout(request.timer);
      try {
        if (error) throw new Error(String(error));
        const [frame] = validateImages([image]);
        const file = await saveFile(new Request('http://localhost/files?name=camera.jpg', {method:'POST', headers:{'Content-Type':frame.type}, body:Buffer.from(frame.data, 'base64')}), root);
        request.resolve(filePath(file, root));
      } catch (err) { request.reject(err instanceof Error ? err : new Error(String(err))); }
    },
  };
}
