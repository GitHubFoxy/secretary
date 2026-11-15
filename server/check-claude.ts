import { Session } from './session';

const session = Session.create('haiku', process.cwd());
let turns = 0;
let lastResult = '';
session.onEvent(event => {
  console.log(JSON.stringify(event));
  if ((event as any).type === 'result') lastResult = (event as any).result;
  if ((event as any).type === 'status') {
    if ((event as any).status !== 'idle') process.exit(1);
    if (++turns === 2) process.exit(lastResult.trim() === 'MAPLE' ? 0 : 1);
  }
});
session.send('Remember the codeword MAPLE. Reply only with OK.');
session.send('What codeword did I ask you to remember? Reply only with the codeword.');
