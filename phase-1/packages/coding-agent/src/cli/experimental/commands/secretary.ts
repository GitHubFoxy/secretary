import { Command, flagOption, stringOption } from "../command.ts";

export interface SecretaryCommand {
	readonly command: "secretary";
	readonly baseUrl?: string;
	readonly credential?: string;
	readonly credentialFile?: string;
	readonly bootstrapToken?: string;
	readonly deviceId?: string;
	readonly displayName?: string;
	readonly platform?: string;
	readonly workerRef?: string;
	readonly once?: boolean;
}

export interface SecretaryCommandContext {
	runSecretary(command: SecretaryCommand): void | Promise<void>;
}

const baseUrlOption = stringOption("--base-url");
const credentialOption = stringOption("--credential");
const credentialFileOption = stringOption("--credential-file");
const bootstrapTokenOption = stringOption("--bootstrap-token");
const deviceIdOption = stringOption("--device-id");
const displayNameOption = stringOption("--display-name");
const platformOption = stringOption("--platform");
const workerRefOption = stringOption("--worker-ref");
const onceOption = flagOption("--once");

export const secretaryCommand = new Command<SecretaryCommand, SecretaryCommandContext>("secretary")
	.option(baseUrlOption)
	.option(credentialOption)
	.option(credentialFileOption)
	.option(bootstrapTokenOption)
	.option(deviceIdOption)
	.option(displayNameOption)
	.option(platformOption)
	.option(workerRefOption)
	.option(onceOption)
	.build((input) => {
		const baseUrl = input.value(baseUrlOption);
		const credential = input.value(credentialOption);
		const credentialFile = input.value(credentialFileOption);
		const bootstrapToken = input.value(bootstrapTokenOption);
		const deviceId = input.value(deviceIdOption);
		const displayName = input.value(displayNameOption);
		const platform = input.value(platformOption);
		const workerRef = input.value(workerRefOption);
		const once = input.value(onceOption) === true;
		const errors: string[] = [];
		if (credential !== undefined && credentialFile !== undefined)
			errors.push("--credential and --credential-file are mutually exclusive");
		if (bootstrapToken !== undefined && credential !== undefined)
			errors.push("--bootstrap-token cannot be combined with --credential");
		if (bootstrapToken !== undefined && credentialFile !== undefined)
			errors.push("--bootstrap-token cannot be combined with --credential-file");
		if (bootstrapToken !== undefined && deviceId === undefined) errors.push("--bootstrap-token requires --device-id");
		if (bootstrapToken !== undefined && displayName === undefined)
			errors.push("--bootstrap-token requires --display-name");
		if (bootstrapToken === undefined && credential === undefined && credentialFile === undefined)
			errors.push("Secretary client requires --credential, --credential-file, or --bootstrap-token");
		if (input.remainingArgs.length > 0)
			errors.push("The experimental secretary command does not accept positional arguments");
		if (errors.length > 0) return { ok: false, errors };
		return {
			ok: true,
			command: {
				command: "secretary",
				...(baseUrl === undefined ? {} : { baseUrl }),
				...(credential === undefined ? {} : { credential }),
				...(credentialFile === undefined ? {} : { credentialFile }),
				...(bootstrapToken === undefined ? {} : { bootstrapToken }),
				...(deviceId === undefined ? {} : { deviceId }),
				...(displayName === undefined ? {} : { displayName }),
				...(platform === undefined ? {} : { platform }),
				...(workerRef === undefined ? {} : { workerRef }),
				...(once ? { once: true } : {}),
			},
		};
	})
	.action((command, context) => context.runSecretary(command));
