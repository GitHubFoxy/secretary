<script>
  let message = '';
  let received = '';
  const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`);
  socket.onmessage = (event) => { received = event.data; };

  function send(event) {
    event.preventDefault();
    socket.send(message);
    message = '';
  }
</script>

<h1>Secretary</h1>
<form onsubmit={send}>
  <label>Сообщение <input bind:value={message}></label>
  <button>Отправить</button>
</form>
<p>Ответ: {received}</p>
