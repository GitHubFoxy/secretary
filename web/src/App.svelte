<script>
  let message = '';
  let received = '';
  let id;
  const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`);
  socket.onmessage = (event) => {
    const data = JSON.parse(event.data);
    if (data.type === 'created') id = data.id;
    if (data.event?.type === 'result') received = data.event.result;
  };

  function create(event) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    socket.send(JSON.stringify({ type: 'create', model: form.get('model'), folder: form.get('folder') }));
  }

  function send(event) {
    event.preventDefault();
    socket.send(JSON.stringify({ type: 'send', id, text: message }));
    message = '';
  }
</script>

<h1>Secretary</h1>
{#if !id}
  <form onsubmit={create}>
    <label>Модель <input name="model" value="haiku" required></label>
    <label>Папка <input name="folder" value="/tmp" required></label>
    <button>Создать сессию</button>
  </form>
{:else}
  <form onsubmit={send}>
    <label>Сообщение <input bind:value={message}></label>
    <button>Отправить</button>
  </form>
  <p>Ответ: {received}</p>
{/if}
