export function filterCommands(commands, text) {
  const match = /^\/([^\s]*)$/.exec(text);
  if (!match) return [];
  const query = match[1].toLocaleLowerCase();
  return commands.filter(command => command.name.toLocaleLowerCase().includes(query));
}
