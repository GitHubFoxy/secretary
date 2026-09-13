import { Command } from "./command.ts";
import { type ClientCommandContext, clientCommand } from "./commands/client.ts";
import { type SecretaryCommandContext, secretaryCommand } from "./commands/secretary.ts";
import { type ServerCommandContext, serverCommand } from "./commands/server.ts";

interface ExperimentalCommandGroup {
	readonly command: "experimental";
}

export type CliContext = ServerCommandContext & ClientCommandContext & SecretaryCommandContext;

const experimentalCommand = new Command<ExperimentalCommandGroup, CliContext>("experimental").build(() => ({
	ok: false,
	errors: ["Expected experimental command: server, client, or secretary"],
}));

export const cli = experimentalCommand.command(serverCommand).command(clientCommand).command(secretaryCommand);
