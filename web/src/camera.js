export function createCamera({request, connected}) {
  let capture = null;
  function announce() {
    if (connected()) request({type:'cameraState', enabled:!!capture}).catch(() => {});
  }
  return {
    set(value) { capture = value; announce(); },
    reconnect: announce,
    receive(msg) {
      if (msg.type !== 'camera_frame') return false;
      try { request({type:'cameraReply', id:msg.id, image:capture ? capture() : undefined, error:capture ? undefined : 'Камера не включена'}).catch(() => {}); }
      catch (err) { request({type:'cameraReply', id:msg.id, error:err.message}).catch(() => {}); }
      return true;
    },
  };
}
