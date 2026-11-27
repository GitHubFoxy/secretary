export const slashToken = text => /(?:^|\s)\/([^\s/]*)$/.exec(text);

export function filterCommands(commands, text) {
  const match = slashToken(text);
  if (!match) return [];
  const query = match[1].toLocaleLowerCase();
  return commands.filter(command => command.name.toLocaleLowerCase().includes(query));
}
