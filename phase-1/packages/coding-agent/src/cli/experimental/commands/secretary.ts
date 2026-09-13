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
	readonly message?: string;
	readonly respondRequest?: string;
	readonly response?: string;
	readonly cancel?: boolean;
	readonly close?: boolean;
	readonly approve?: string;
	readonly deny?: string;
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
const messageOption = stringOption("--message");
const respondRequestOption = stringOption("--respond-request");
const responseOption = stringOption("--response");
const cancelOption = flagOption("--cancel");
const closeOption = flagOption("--close");
const approveOption = stringOption("--approve");
const denyOption = stringOption("--deny");
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
	.option(messageOption)
	.option(respondRequestOption)
	.option(responseOption)
	.option(cancelOption)
	.option(closeOption)
	.option(approveOption)
	.option(denyOption)
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
		const message = input.value(messageOption);
		const respondRequest = input.value(respondRequestOption);
		const response = input.value(responseOption);
		const cancel = input.value(cancelOption) === true;
		const close = input.value(closeOption) === true;
		const approve = input.value(approveOption);
		const deny = input.value(denyOption);
		const once = input.value(onceOption) === true;
		const actions = [message !== undefined, respondRequest !== undefined, cancel, close, approve !== undefined, deny !== undefined].filter(Boolean).length;
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
		if (actions > 1) errors.push("Secretary client accepts only one Worker or Approval action");
		if ((message !== undefined || respondRequest !== undefined || cancel || close) && workerRef === undefined)
			errors.push("Worker actions require --worker-ref");
		if (respondRequest !== undefined && response === undefined) errors.push("--respond-request requires --response");
		if (respondRequest === undefined && response !== undefined) errors.push("--response requires --respond-request");
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
				...(message === undefined ? {} : { message }),
				...(respondRequest === undefined ? {} : { respondRequest }),
				...(response === undefined ? {} : { response }),
				...(cancel ? { cancel: true } : {}),
				...(close ? { close: true } : {}),
				...(approve === undefined ? {} : { approve }),
				...(deny === undefined ? {} : { deny }),
				...(once ? { once: true } : {}),
			},
		};
	})
	.action((command, context) => context.runSecretary(command));
