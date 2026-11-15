import { Session } from './session';

const session = Session.create('gpt-6-luna', process.cwd(), 'codex');
let turns = 0;
let lastText = '';
session.onEvent((event: any) => {
  if (event.method === 'item/completed' && event.params.item.type === 'agentMessage') lastText = event.params.item.text;
  if (event.type === 'status') {
    if (event.status !== 'idle') process.exit(1);
    if (++turns === 1) {
      session.setModel('gpt-6-sol');
      session.send('What was the codeword? Reply with just the codeword.');
    } else {
      console.log('Codex remembered:', lastText);
      process.exit(lastText.trim().toUpperCase() === 'MAPLE' ? 0 : 1);
    }
  }
});
session.send('Remember the codeword MAPLE. Reply only with OK.');
