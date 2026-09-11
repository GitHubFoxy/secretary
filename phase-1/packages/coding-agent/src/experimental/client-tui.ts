import { resolve } from "node:path";
import {
	combineFacetLoaders,
	createFacetHost,
	defineFacet,
	type FacetHost,
	type FacetLoader,
	type JsonValue,
	type LoadedFacets,
} from "@earendil-works/chord";
import { BACKGROUND_CONTEXT } from "@earendil-works/chord/context";
import { type LaneSnapshot, type LaneTranscriptSnapshot, reduceLaneSnapshot } from "@earendil-works/pi-agent-core";
import {
	type AutocompleteItem,
	type AutocompleteProvider,
	CombinedAutocompleteProvider,
	type Component,
	Container,
	type OverlayHandle,
	type SelectItem,
	SelectList,
	setKeybindings,
	Text,
	type TUI,
} from "@earendil-works/pi-tui";
import type { ClientCommand } from "../cli/experimental/commands/client.ts";
import { getAgentDir } from "../config.ts";
import type { EntryRenderer, MessageRenderer } from "../core/extensions/types.ts";
import { KeybindingsManager } from "../core/keybindings.ts";
import { DefaultResourceLoader } from "../core/resource-loader.ts";
import { SettingsManager } from "../core/settings-manager.ts";
import { createChatViewport } from "../modes/interactive/chat-viewport.ts";
import { CustomEditor } from "../modes/interactive/components/custom-editor.ts";
import type { ToolRenderers } from "../modes/interactive/components/tool-execution.ts";
import { getEditorTheme, setRegisteredThemes, stopThemeWatcher, theme } from "../modes/interactive/theme/theme.ts";
import { InteractiveThemeController } from "../modes/interactive/theme/theme-controller.ts";
import { createInteractiveTui } from "../modes/interactive/tui-renderer.ts";
import { type OpenClientRuntimeOptions, openClientRuntime } from "./client-runtime.ts";
import { ExperimentalChatView } from "./client-tui-chat.ts";
import { createPresentationFacetLoaders } from "./plugins/bundled.ts";
import { AgentController, type AgentOperationResponse, type AgentQueueResponse } from "./services/agent-controller.ts";
import type {
	ServerConnectionState,
	ServerServiceSource,
	SessionAttachmentState,
	SessionServiceSource,
} from "./services/connection.ts";
import {
	type ExtensionAutocompleteResult,
	ExtensionCommands,
	type ExtensionCommands as ExtensionCommandsService,
	type ExtensionUIPrompt,
	type ExtensionUIState,
} from "./services/extension-commands.ts";
import { PresentationPlugins } from "./services/plugins.ts";
import { PresentationUI } from "./services/presentation-ui.ts";
import { SessionDirectory, SessionManagement, type SessionSummary } from "./services/sessions.ts";
import { SlashCommands } from "./services/slash-commands.ts";
import {
	createBuiltInSlashCommandsFacet,
	createSlashCommandsRuntimeFacet,
} from "./services/slash-commands-provider.ts";
import { Transcript, type Transcript as TranscriptService } from "./services/transcript.ts";

export interface RunClientTuiOptions extends OpenClientRuntimeOptions {
	readonly facetLoader?: FacetLoader;
}

export interface ClientTuiServer {
	readonly serverId: string;
	readonly radius: boolean;
	readonly server: ServerServiceSource;
	readonly session: SessionServiceSource;
}

interface SessionFeature {
	readonly serverId: string;
	readonly session: SessionServiceSource;
	readonly transcript: TranscriptService;
}

interface PreparedClientSession {
	readonly server: ClientTuiServer;
	readonly summary: SessionSummary;
	readonly presentationPlugins: JsonValue;
}

interface PendingSelection {
	readonly title: string;
	readonly items: readonly SelectItem[];
	readonly selectedValue?: string;
	resolve(value: string | undefined): void;
}

const selectTheme = {
	selectedPrefix: (text: string) => theme.fg("accent", text),
	selectedText: (text: string) => theme.fg("accent", text),
	description: (text: string) => theme.fg("muted", text),
	scrollInfo: (text: string) => theme.fg("dim", text),
	noMatch: (text: string) => theme.fg("warning", text),
};

/** Service-only presentation driven by a replicated main-lane snapshot. */
export class ExperimentalClientTui implements Component {
	readonly #ui: TUI;
	readonly #requestRender: () => void;
	readonly #finish: () => void;
	readonly #documentContainer = new Container();
	readonly #sessionHeading = new Text("", 1, 0);
	readonly #extensionHeader = new Text("", 1, 0);
	readonly #pendingMessagesContainer = new Container();
	readonly #extensionWidgetsAbove = new Container();
	readonly #extensionWidgetsBelow = new Container();
	readonly #statusContainer = new Container();
	readonly #editorContainer = new Container();
	readonly #footerComponent = new Text("", 1, 0);
	readonly #layoutRoot: Component;
	readonly #sharedFacets: LoadedFacets;
	readonly #extensionRenderers: Readonly<Record<string, ToolRenderers>>;
	readonly #messageRenderers: ReadonlyMap<string, MessageRenderer>;
	readonly #entryRenderers: ReadonlyMap<string, EntryRenderer>;
	readonly #keybindings = KeybindingsManager.create();
	#presentationFacets: LoadedFacets | undefined;
	#facetHost: FacetHost | undefined;
	#facetReloadTail = Promise.resolve();
	#session: SessionFeature | undefined;
	#slashCommands: SlashCommands | undefined;
	#controller: AgentController | undefined;
	readonly #chatInput: CustomEditor;
	#selectList: SelectList | undefined;
	#selection: PendingSelection | undefined;
	#screen: "select" | "chat" = "chat";
	#selectedServerId: string | undefined;
	#sessionId: string | undefined;
	#status = "Starting Session…";
	#busy = false;
	#closed = false;
	#closePromise: Promise<void> | undefined;
	#recoveryTransition: Promise<void> = Promise.resolve();
	#laneUnsubscribe: (() => void) | undefined;
	#laneSyncTimer: NodeJS.Timeout | undefined;
	#chatView: ExperimentalChatView | undefined;
	#extensionCommands: ExtensionCommandsService | undefined;
	#extensionUiState: ExtensionUIState | undefined;
	#extensionNotice = "";
	#extensionNoticeId = 0;
	#extensionFooterLines: string[] | undefined;
	#extensionPrompt: ExtensionUIPrompt | null = null;
	#lastExtensionEditorSubmit: string | undefined;
	#extensionPromptId: string | undefined;
	#extensionPromptSelect: SelectList | undefined;
	#extensionPromptEditor: CustomEditor | undefined;
	#extensionOverlayPromptId: string | undefined;
	#extensionOverlayLines = "";
	#extensionOverlayHandle: OverlayHandle | undefined;
	#toolsExpanded = false;

	private constructor(
		ui: TUI,
		requestRender: () => void,
		finish: () => void,
		loadedFacets: LoadedFacets,
		extensionRenderers: Readonly<Record<string, ToolRenderers>>,
		messageRenderers: ReadonlyMap<string, MessageRenderer>,
		entryRenderers: ReadonlyMap<string, EntryRenderer>,
	) {
		this.#ui = ui;
		this.#requestRender = requestRender;
		this.#finish = finish;
		this.#sharedFacets = loadedFacets;
		this.#extensionRenderers = extensionRenderers;
		this.#messageRenderers = messageRenderers;
		this.#entryRenderers = entryRenderers;
		setKeybindings(this.#keybindings);
		this.#chatInput = new CustomEditor(ui, getEditorTheme(), this.#keybindings, { paddingX: 1 });
		this.#chatInput.onSubmit = (message) => void this.#runPrompt(message);
		this.#chatInput.onEscape = () => this.#interrupt();
		this.#chatInput.onCtrlD = finish;
		this.#chatInput.onAction("app.clear", finish);
		this.#chatInput.onAction("app.model.select", () => void this.#executeSlashCommand("model", ""));
		this.#chatInput.onAction("app.message.followUp", () => {
			const text = this.#chatInput.getText().trim();
			if (text.length === 0) return;
			this.#chatInput.setText("");
			void this.#queueFollowUp(text);
		});
		this.#editorContainer.addChild(this.#chatInput);
		this.#layoutRoot = createChatViewport({
			document: this.#documentContainer,
			pendingMessages: this.#pendingMessagesContainer,
			status: this.#statusContainer,
			editor: this.#editorContainer,
			footer: this.#footerComponent,
			widgetsAbove: this.#extensionWidgetsAbove,
			widgetsBelow: this.#extensionWidgetsBelow,
			scrollbarTrackStyle: (text) => theme.fg("scrollbarTrack", text),
			scrollbarThumbStyle: (text) => theme.fg("scrollbarThumb", text),
		}).root;
		this.#rebuild();
	}

	static async create(options: {
		readonly command: ClientCommand;
		readonly ui: TUI;
		readonly servers: readonly ClientTuiServer[];
		readonly facetLoader?: FacetLoader;
		readonly extensionRenderers?: Readonly<Record<string, ToolRenderers>>;
		readonly messageRenderers?: ReadonlyMap<string, MessageRenderer>;
		readonly entryRenderers?: ReadonlyMap<string, EntryRenderer>;
		requestRender(): void;
		finish(): void;
	}): Promise<ExperimentalClientTui> {
		const prepared = await prepareClientSession(options.command, options.servers);
		const loadedFacets = await combineFacetLoaders(
			options.facetLoader === undefined ? [] : [options.facetLoader],
		).load();
		const component = new ExperimentalClientTui(
			options.ui,
			options.requestRender,
			options.finish,
			loadedFacets,
			options.extensionRenderers ?? {},
			options.messageRenderers ?? new Map(),
			options.entryRenderers ?? new Map(),
		);
		try {
			await component.#start(prepared);
			await component.#openPreparedSession(prepared);
			return component;
		} catch (error) {
			try {
				await component.close();
			} catch (cleanupError) {
				throw new AggregateError([error, cleanupError], "Experimental TUI startup and cleanup failed");
			}
			throw error;
		}
	}

	get layoutRoot(): Component {
		return this.#layoutRoot;
	}

	render(width: number): string[] {
		return [
			...this.#documentContainer.render(width),
			...this.#pendingMessagesContainer.render(width),
			...this.#statusContainer.render(width),
			...this.#extensionWidgetsAbove.render(width),
			...this.#editorContainer.render(width),
			...this.#extensionWidgetsBelow.render(width),
			...this.#footerComponent.render(width),
		];
	}

	handleInput(data: string): void {
		this.#handleInput(data, false);
	}

	#handleInput(data: string, bypassTerminalInput: boolean, bypassExtensionEditor = false): void {
		const operation = this.#laneSnapshot()?.operation;
		const interrupt = this.#keybindings.matches(data, "app.interrupt");
		const autocompleteActive = this.#chatInput.isShowingAutocomplete();
		const extensionEditorActive = this.#extensionUiState?.editor !== undefined && !bypassExtensionEditor;
		if (
			interrupt &&
			operation !== null &&
			operation !== undefined &&
			!this.#extensionPrompt &&
			!extensionEditorActive &&
			!autocompleteActive
		) {
			this.#interrupt();
			return;
		}
		if (
			!bypassTerminalInput &&
			!this.#extensionPrompt &&
			this.#extensionUiState?.editor === undefined &&
			!autocompleteActive &&
			this.#extensionUiState?.terminalInput === true
		) {
			const commands = this.#extensionCommands;
			if (commands) {
				void commands
					.inputUI("", data, BACKGROUND_CONTEXT)
					.then((consumed) => {
						if (!consumed && !this.#closed) this.#handleInput(data, true);
					})
					.catch((error: unknown) => {
						this.#status = `Error: ${message(error)}`;
						this.#rebuild();
					});
				return;
			}
		}
		if (this.#extensionPrompt) {
			if (this.#extensionPrompt.kind === "custom") {
				const commands = this.#extensionCommands;
				if (commands) void commands.inputUI(this.#extensionPrompt.id, data, BACKGROUND_CONTEXT);
			} else if (this.#extensionPrompt.kind === "select" || this.#extensionPrompt.kind === "confirm") {
				this.#extensionPromptSelect?.handleInput(data);
			} else {
				this.#extensionPromptEditor?.handleInput(data);
			}
			this.#requestRender();
			return;
		}
		if (!bypassExtensionEditor && this.#extensionUiState?.editor !== undefined) {
			const commands = this.#extensionCommands;
			if (commands) {
				void commands.inputUI("", data, BACKGROUND_CONTEXT).then(
					(consumed) => {
						if (!consumed && !this.#closed) this.#handleInput(data, bypassTerminalInput, true);
					},
					(error: unknown) => {
						this.#status = `Error: ${message(error)}`;
						this.#rebuild();
					},
				);
			}
			return;
		}
		if (this.#keybindings.matches(data, "app.tools.expand")) {
			this.#toggleToolsExpanded();
			return;
		}

		if (this.#busy) {
			if (
				this.#keybindings.matches(data, "app.clear") ||
				(this.#chatInput.getText().length === 0 && this.#keybindings.matches(data, "app.exit"))
			) {
				this.#finish();
			}
			return;
		}
		if (this.#screen === "chat") {
			this.#chatInput.handleInput(data);
			this.#requestRender();
			return;
		}
		this.#selectList?.handleInput(data);
	}

	invalidate(): void {
		this.#layoutRoot.invalidate();
	}

	dispose(): void {
		void this.close().catch(() => {});
	}

	refreshTheme(): void {
		const snapshot = this.#laneSnapshot();
		if (snapshot !== undefined) this.#chatView?.refreshTheme(snapshot);
		this.#rebuild();
	}

	showError(error: string): void {
		this.#status = `Error: ${error}`;
		this.#rebuild();
	}

	close(): Promise<void> {
		this.#closePromise ??= this.#close();
		return this.#closePromise;
	}

	async #start(prepared: PreparedClientSession): Promise<void> {
		const server = prepared.server;
		let presentationFacets = await combineFacetLoaders(
			createPresentationFacetLoaders(prepared.presentationPlugins),
		).load();
		this.#presentationFacets = presentationFacets;
		let facetHost!: FacetHost;
		const reloadPresentationPlugins = (data: JsonValue): Promise<void> => {
			const operation = this.#facetReloadTail.then(async () => {
				const candidate = await combineFacetLoaders(createPresentationFacetLoaders(data)).load();
				try {
					await facetHost.reload(candidate.facets);
				} catch (error) {
					try {
						await candidate.dispose();
					} catch (cleanupError) {
						throw new AggregateError([error, cleanupError], "TUI plugin reload and cleanup failed");
					}
					throw error;
				}
				const retired = presentationFacets;
				presentationFacets = candidate;
				this.#presentationFacets = candidate;
				await retired.dispose();
			});
			this.#facetReloadTail = operation.catch(() => {});
			return operation;
		};
		const presentationBridgeFacet = defineFacet({
			id: "@pi/presentation-bridge",
			setup: (env) => {
				env.provide(PresentationUI, {
					select: (title, items, selectedValue) =>
						this.#select(
							title,
							items.map((item) => ({ ...item })),
							selectedValue,
						),
					showStatus: (status) => {
						this.#status = status;
						this.#rebuild();
					},
				});
				const commands = env.use(SlashCommands);
				const controller = env.use(AgentController);
				const extensionCommands = env.use(ExtensionCommands);
				const transcript = env.use(Transcript);
				const sessionFeature: SessionFeature = {
					serverId: server.serverId,
					session: server.session,
					transcript,
				};
				env.onActivate(async () => {
					if (this.#session !== undefined || this.#slashCommands !== undefined || this.#controller !== undefined) {
						throw new Error("Presentation services are already active");
					}
					this.#session = sessionFeature;
					this.#slashCommands = commands;
					this.#controller = controller;
					this.#extensionCommands = extensionCommands;
					env.own(() => {
						if (this.#extensionCommands === extensionCommands) this.#extensionCommands = undefined;
					});
					env.own(extensionCommands.uiState.subscribe((state) => this.#handleExtensionUIState(state)));
					if (extensionCommands.uiState.value !== undefined)
						this.#handleExtensionUIState(extensionCommands.uiState.value);
					env.own(() => {
						if (this.#session === sessionFeature) this.#session = undefined;
						if (this.#slashCommands === commands) this.#slashCommands = undefined;
						if (this.#controller === controller) this.#controller = undefined;
					});
					env.own(commands.subscribe(() => this.#updateAutocomplete()));
					for (const command of await extensionCommands.list(BACKGROUND_CONTEXT)) {
						env.own(
							commands.replace({
								...command,
								getArgumentCompletions: (prefix) =>
									extensionCommands.complete(command.name, prefix, BACKGROUND_CONTEXT),
								run: async (args) => {
									const notifications = await extensionCommands.run(command.name, args, BACKGROUND_CONTEXT);
									if (notifications.length > 0) {
										this.#status = notifications.join("\n");
										this.#rebuild();
									}
									return undefined;
								},
							}),
						);
					}
					this.#updateAutocomplete();
					if (server.radius) {
						env.own(
							server.server.connection.subscribe((state) => this.#handleConnectionState(server.serverId, state)),
						);
						env.own(
							server.session.attachment.subscribe((state) => this.#handleAttachmentState(sessionFeature, state)),
						);
					}
				});
			},
		});
		facetHost = await createFacetHost({
			facets: [
				createSlashCommandsRuntimeFacet(),
				presentationBridgeFacet,
				createBuiltInSlashCommandsFacet({ reloadPresentationPlugins }),
				...this.#sharedFacets.facets,
				...presentationFacets.facets,
			],
			serviceSources: [server.server, server.session],
		});
		this.#facetHost = facetHost;
	}

	async #openPreparedSession(prepared: PreparedClientSession): Promise<void> {
		const feature = this.#session;
		if (feature === undefined) throw new Error(`No Session service is available for ${prepared.server.serverId}`);
		await feature.session.whenAttached(prepared.summary.sessionId, BACKGROUND_CONTEXT);
		this.#selectedServerId = feature.serverId;
		this.#sessionId = prepared.summary.sessionId;
		this.#updateAutocomplete();
		await this.#openLane(feature);
		this.#screen = "chat";
		this.#status = "";
		this.#rebuild();
	}

	async #close(): Promise<void> {
		this.#closed = true;
		this.#completeSelection(undefined);
		const errors: unknown[] = [];
		try {
			await this.#recoveryTransition;
			await this.#closeLane();
			await this.#facetReloadTail;
		} catch (error) {
			errors.push(error);
		}
		this.#extensionOverlayHandle?.hide();
		this.#extensionOverlayHandle = undefined;
		this.#extensionOverlayPromptId = undefined;
		if (this.#facetHost !== undefined) {
			try {
				await this.#facetHost.dispose();
			} catch (error) {
				errors.push(error);
			}
			this.#facetHost = undefined;
		}
		const generations = [this.#presentationFacets, this.#sharedFacets].filter(
			(generation): generation is LoadedFacets => generation !== undefined,
		);
		this.#presentationFacets = undefined;
		const results = await Promise.allSettled(generations.map((generation) => generation.dispose()));
		errors.push(...results.flatMap((result) => (result.status === "rejected" ? [result.reason] : [])));
		if (errors.length === 1) throw errors[0];
		if (errors.length > 1) throw new AggregateError(errors, "Failed to dispose experimental TUI facets");
	}

	#handleExtensionUIState(state: ExtensionUIState): void {
		try {
			this.#applyExtensionUIState(state);
		} catch (error) {
			console.error("Extension UI state render failed", error);
			this.#status = `Extension UI error: ${message(error)}`;
			this.#requestRender();
		}
	}

	#applyExtensionUIState(state: ExtensionUIState): void {
		this.#extensionUiState = state;
		if (state.editorSubmit !== undefined && state.editorSubmit.id !== this.#lastExtensionEditorSubmit) {
			this.#lastExtensionEditorSubmit = state.editorSubmit.id;
			void this.#runPrompt(state.editorSubmit.text);
		}
		const latestNotification = state.notifications.at(-1);
		if (latestNotification && latestNotification.id > this.#extensionNoticeId) {
			this.#extensionNoticeId = latestNotification.id;
			this.#extensionNotice = latestNotification.message;
		}
		this.#extensionPrompt = state.prompt;
		this.#syncExtensionOverlay(state.prompt);
		this.#extensionHeader.setText(state.header?.join("\n") ?? "");
		this.#extensionWidgetsAbove.clear();
		this.#extensionWidgetsBelow.clear();
		for (const [key, widget] of Object.entries(state.widgets)) {
			const component = new Text(widget.content.join("\n"), 1, 0);
			if (widget.placement === "belowEditor") this.#extensionWidgetsBelow.addChild(component);
			else this.#extensionWidgetsAbove.addChild(component);
			void key;
		}
		this.#extensionFooterLines = state.footer;
		if (state.title !== undefined) this.#ui.terminal.setTitle(state.title);
		this.#toolsExpanded = state.toolsExpanded ?? false;
		this.#chatView?.setHiddenThinkingLabel(state.hiddenThinkingLabel);
		this.#chatView?.setToolsExpanded(this.#toolsExpanded);
		this.#chatView?.setWorkingPresentation({
			...(state.workingMessage === undefined ? {} : { message: state.workingMessage }),
			visible: state.workingVisible,
			...(state.workingIndicator === undefined ? {} : { indicator: state.workingIndicator }),
		});
		if (this.#extensionPromptId !== state.prompt?.id) {
			this.#extensionPromptId = state.prompt?.id;
			this.#extensionPromptSelect = undefined;
			this.#extensionPromptEditor = undefined;
		}
		this.#rebuild();
	}

	#syncExtensionOverlay(prompt: ExtensionUIPrompt | null): void {
		const overlay = prompt?.kind === "custom" && prompt.overlay ? prompt : undefined;
		const lines = overlay?.lines.join("\n") ?? "";
		if (
			overlay === undefined ||
			this.#extensionOverlayPromptId !== overlay.id ||
			this.#extensionOverlayLines !== lines
		) {
			this.#extensionOverlayHandle?.hide();
			this.#extensionOverlayHandle = undefined;
			this.#extensionOverlayPromptId = undefined;
			this.#extensionOverlayLines = "";
		}
		if (overlay !== undefined && this.#extensionOverlayHandle === undefined) {
			this.#extensionOverlayPromptId = overlay.id;
			this.#extensionOverlayLines = lines;
			this.#extensionOverlayHandle = this.#ui.showOverlay(new Text(lines, 1, 1));
		}
	}

	#respondExtensionPrompt(value: string | boolean | null): void {
		const prompt = this.#extensionPrompt;
		const commands = this.#extensionCommands;
		if (!prompt || !commands) return;
		void commands.respondUI(prompt.id, value, BACKGROUND_CONTEXT).catch((error: unknown) => {
			this.#status = `Error: ${message(error)}`;
			this.#rebuild();
		});
	}

	#renderExtensionPrompt(): Component {
		const prompt = this.#extensionPrompt;
		if (!prompt) return this.#chatInput;
		const container = new Container();
		if (prompt.kind === "custom") {
			if (prompt.overlay) return new Text("", 1, 0);
			container.addChild(new Text(prompt.lines.join("\n"), 1, 1));
			return container;
		}
		container.addChild(new Text(theme.bold(prompt.title), 1, 1));
		if (prompt.kind === "select" || prompt.kind === "confirm") {
			if (!this.#extensionPromptSelect) {
				this.#extensionPromptSelect = new SelectList(
					prompt.options.map((value) => ({ value, label: value })),
					Math.min(Math.max(prompt.options.length, 1), 12),
					selectTheme,
				);
				this.#extensionPromptSelect.onSelect = (item) =>
					this.#respondExtensionPrompt(prompt.kind === "confirm" ? item.value === "Yes" : item.value);
				this.#extensionPromptSelect.onCancel = () => this.#respondExtensionPrompt(null);
			}
			container.addChild(this.#extensionPromptSelect);
			return container;
		}
		if (prompt.kind === "input" && prompt.placeholder !== undefined) {
			container.addChild(new Text(theme.fg("dim", prompt.placeholder), 1, 1));
		}
		if (!this.#extensionPromptEditor) {
			this.#extensionPromptEditor = new CustomEditor(this.#ui, getEditorTheme(), this.#keybindings, { paddingX: 1 });
			this.#extensionPromptEditor.setText(("initial" in prompt ? prompt.initial : undefined) ?? "");
			this.#extensionPromptEditor.onSubmit = (value) => this.#respondExtensionPrompt(value);
			this.#extensionPromptEditor.onEscape = () => this.#respondExtensionPrompt(null);
		}
		container.addChild(this.#extensionPromptEditor);
		return container;
	}

	#rebuild(): void {
		this.#sessionHeading.setText(
			this.#sessionId === undefined || this.#selectedServerId === undefined
				? ""
				: theme.fg("dim", `Server: ${this.#selectedServerId}\nSession: ${this.#sessionId}`),
		);
		this.#statusContainer.clear();
		if (this.#status.length > 0) {
			this.#statusContainer.addChild(new Text(theme.fg("dim", this.#status), 1, 0));
		}
		if (this.#extensionNotice.length > 0) {
			this.#statusContainer.addChild(new Text(theme.fg("dim", this.#extensionNotice), 1, 0));
		}
		const extensionStatuses = Object.values(this.#extensionUiState?.statuses ?? {});
		if (extensionStatuses.length > 0) {
			this.#statusContainer.addChild(new Text(extensionStatuses.join(" · "), 1, 0));
		}
		if (this.#chatView !== undefined) this.#statusContainer.addChild(this.#chatView.status);
		this.#footerComponent.setText(theme.fg("dim", this.#extensionFooterLines?.join("\n") ?? this.#footer()));
		this.#editorContainer.clear();
		if (this.#extensionPrompt !== null) {
			this.#chatInput.focused = false;
			this.#editorContainer.addChild(this.#renderExtensionPrompt());
		} else if (this.#extensionUiState?.editor !== undefined) {
			this.#chatInput.focused = false;
			this.#editorContainer.addChild(new Text(this.#extensionUiState.editor.lines.join("\n"), 1, 0));
		} else if (this.#screen === "select" && this.#selection !== undefined) {
			this.#chatInput.focused = false;
			const selector = new Container();
			selector.addChild(new Text(theme.bold(this.#selection.title), 1, 1));
			const items = [...this.#selection.items];
			this.#selectList = new SelectList(items, Math.min(Math.max(items.length, 1), 12), selectTheme);
			const selectedIndex = items.findIndex((item) => item.value === this.#selection?.selectedValue);
			if (selectedIndex >= 0) this.#selectList.setSelectedIndex(selectedIndex);
			this.#selectList.onSelect = (item) => this.#completeSelection(item.value);
			this.#selectList.onCancel = () => this.#completeSelection(undefined);
			selector.addChild(this.#selectList);
			this.#editorContainer.addChild(selector);
		} else {
			this.#selectList = undefined;
			this.#chatInput.focused = !this.#busy;
			this.#editorContainer.addChild(this.#chatInput);
		}
		this.#layoutRoot.invalidate();
		this.#requestRender();
	}

	#select(title: string, items: readonly SelectItem[], selectedValue?: string): Promise<string | undefined> {
		if (this.#selection !== undefined) throw new Error("A slash command selector is already active");
		return new Promise((resolve) => {
			this.#selection = { title, items, ...(selectedValue === undefined ? {} : { selectedValue }), resolve };
			this.#screen = "select";
			this.#rebuild();
		});
	}

	#completeSelection(value: string | undefined): void {
		const selection = this.#selection;
		if (selection === undefined) return;
		this.#selection = undefined;
		this.#screen = "chat";
		selection.resolve(value);
		if (!this.#closed) this.#rebuild();
	}

	#updateAutocomplete(): void {
		const commands = this.#selectedSlashCommands()?.list() ?? [];
		const baseProvider = new CombinedAutocompleteProvider(
			commands.map((command) => ({
				name: command.name,
				description: command.description,
				...(command.argumentHint === undefined ? {} : { argumentHint: command.argumentHint }),
				...(command.getArgumentCompletions === undefined
					? {}
					: {
							getArgumentCompletions: async (prefix: string) => {
								const items = await command.getArgumentCompletions!(prefix);
								return items === null ? null : [...items];
							},
						}),
			})),
			process.cwd(),
		);
		const extensionCommands = this.#extensionCommands;
		if (extensionCommands === undefined) {
			this.#chatInput.setAutocompleteProvider(baseProvider);
			this.#requestRender();
			return;
		}
		const completionKey = (item: AutocompleteItem): string => `${item.value}\u0000${item.label}`;
		let cachedCompletions = new Map<string, ExtensionAutocompleteResult>();
		let cachedSignature = "";
		let cachedFileCompletion: boolean | undefined;
		const provider: AutocompleteProvider = {
			triggerCharacters: [],
			async getSuggestions(lines, cursorLine, cursorCol, options) {
				const baseSuggestions = await baseProvider.getSuggestions(lines, cursorLine, cursorCol, options);
				const baseCompletions: Record<string, ExtensionAutocompleteResult> = {};
				if (baseSuggestions !== null) {
					for (const item of baseSuggestions.items) {
						baseCompletions[completionKey(item)] = baseProvider.applyCompletion(
							lines,
							cursorLine,
							cursorCol,
							item,
							baseSuggestions.prefix,
						);
					}
				}
				try {
					const baseShouldTriggerFileCompletion = baseProvider.shouldTriggerFileCompletion?.(
						lines,
						cursorLine,
						cursorCol,
					);
					const request = {
						lines: [...lines],
						cursorLine,
						cursorCol,
						...(options.force === undefined ? {} : { force: options.force }),
						baseSuggestions:
							baseSuggestions === null
								? null
								: { items: [...baseSuggestions.items], prefix: baseSuggestions.prefix },
						baseCompletions,
						...(baseShouldTriggerFileCompletion === undefined ? {} : { baseShouldTriggerFileCompletion }),
					};
					const remote = await extensionCommands.autocomplete(request, BACKGROUND_CONTEXT);
					cachedCompletions = new Map(Object.entries(remote.completions).map(([key, value]) => [key, value]));
					cachedSignature = JSON.stringify({ lines, cursorLine, cursorCol });
					cachedFileCompletion = remote.shouldTriggerFileCompletion;
					return remote.suggestions === null
						? null
						: { items: [...remote.suggestions.items], prefix: remote.suggestions.prefix };
				} catch {
					cachedCompletions = new Map();
					cachedSignature = "";
					cachedFileCompletion = undefined;
					return baseSuggestions;
				}
			},
			applyCompletion(lines, cursorLine, cursorCol, item, prefix) {
				const signature = JSON.stringify({ lines, cursorLine, cursorCol });
				return signature === cachedSignature
					? (cachedCompletions.get(completionKey(item)) ??
							baseProvider.applyCompletion(lines, cursorLine, cursorCol, item, prefix))
					: baseProvider.applyCompletion(lines, cursorLine, cursorCol, item, prefix);
			},
			shouldTriggerFileCompletion(lines, cursorLine, cursorCol) {
				const signature = JSON.stringify({ lines, cursorLine, cursorCol });
				return signature === cachedSignature && cachedFileCompletion !== undefined
					? cachedFileCompletion
					: (baseProvider.shouldTriggerFileCompletion?.(lines, cursorLine, cursorCol) ?? true);
			},
		};
		this.#chatInput.setAutocompleteProvider(provider);
		void extensionCommands.getAutocompleteTriggerCharacters(BACKGROUND_CONTEXT).then(
			(triggerCharacters) => {
				provider.triggerCharacters = [...new Set([...provider.triggerCharacters!, ...triggerCharacters])];
				this.#chatInput.setAutocompleteProvider(provider);
				this.#requestRender();
			},
			() => {},
		);
		this.#requestRender();
	}

	#selectedSlashCommands(): SlashCommands | undefined {
		return this.#slashCommands;
	}

	#handleConnectionState(serverId: string, state: ServerConnectionState): void {
		if (this.#closed || this.#selectedServerId !== serverId) return;
		if (state.status === "connected") {
			if (this.#laneUnsubscribe === undefined) {
				this.#busy = true;
				this.#status = "Reattaching Session…";
				this.#rebuild();
			}
			return;
		}
		this.#busy = true;
		this.#status = state.status === "connecting" ? "Reconnecting to Radius…" : "Radius disconnected; retrying…";
		this.#queueRecovery(() => this.#closeLane());
		this.#rebuild();
	}

	#handleAttachmentState(feature: SessionFeature, state: SessionAttachmentState): void {
		if (this.#closed || this.#selectedServerId !== feature.serverId || this.#sessionId === undefined) return;
		if (state.status === "attached" && state.sessionId === this.#sessionId) {
			this.#queueRecovery(async () => {
				if (this.#laneUnsubscribe === undefined) await this.#openLane(feature);
				this.#busy = false;
				this.#status = "";
				this.#rebuild();
			});
			return;
		}
		if (state.status === "attaching" && state.sessionId === this.#sessionId) {
			this.#busy = true;
			this.#status = "Reattaching Session…";
			this.#rebuild();
		}
	}

	#queueRecovery(operation: () => Promise<void>): void {
		this.#recoveryTransition = this.#recoveryTransition
			.then(async () => {
				if (!this.#closed) await operation();
			})
			.catch((error: unknown) => {
				if (this.#closed) return;
				this.#busy = true;
				this.#status = `Reconnect error: ${message(error)}`;
				this.#rebuild();
			});
	}

	async #openLane(feature: SessionFeature): Promise<void> {
		await this.#closeLane();
		const view = new ExperimentalChatView(
			this.#ui,
			process.cwd(),
			this.#extensionRenderers,
			this.#messageRenderers,
			this.#entryRenderers,
		);
		this.#chatView = view;
		if (this.#extensionUiState) {
			view.setHiddenThinkingLabel(this.#extensionUiState.hiddenThinkingLabel);
			view.setToolsExpanded(this.#toolsExpanded);
			view.setWorkingPresentation({
				...(this.#extensionUiState.workingMessage === undefined
					? {}
					: { message: this.#extensionUiState.workingMessage }),
				visible: this.#extensionUiState.workingVisible,
				...(this.#extensionUiState.workingIndicator === undefined
					? {}
					: { indicator: this.#extensionUiState.workingIndicator }),
			});
		}
		this.#documentContainer.addChild(this.#extensionHeader);
		this.#documentContainer.addChild(this.#sessionHeading);
		this.#documentContainer.addChild(view.transcript);
		this.#pendingMessagesContainer.addChild(view.pendingMessages);
		let renderedSnapshot: LaneSnapshot | undefined;
		this.#laneUnsubscribe = feature.transcript.state.subscribe((value) => {
			if (value.snapshot === null) return;
			if (renderedSnapshot === undefined || value.event === null) {
				renderedSnapshot = structuredClone(value.snapshot) as LaneSnapshot;
			} else if (reduceLaneSnapshot(renderedSnapshot, value.event) === "rebase") {
				renderedSnapshot = structuredClone(value.snapshot) as LaneSnapshot;
			}
			view.apply(renderedSnapshot as LaneTranscriptSnapshot);
			this.#rebuild();
		});
		let syncing = false;
		this.#laneSyncTimer = setInterval(() => {
			if (syncing || this.#closed || this.#chatView !== view) return;
			syncing = true;
			void feature.transcript
				.snapshot(BACKGROUND_CONTEXT)
				.then(
					(snapshot) => {
						renderedSnapshot = structuredClone(snapshot) as LaneSnapshot;
						view.apply(renderedSnapshot as LaneTranscriptSnapshot);
						this.#rebuild();
					},
					() => {},
				)
				.finally(() => {
					syncing = false;
				});
		}, 500);
		this.#laneSyncTimer.unref();
		if (feature.transcript.state.value?.snapshot === null || feature.transcript.state.value?.snapshot === undefined) {
			await this.#closeLane();
			throw new Error("Transcript has no initialized snapshot");
		}
	}

	async #closeLane(): Promise<void> {
		this.#laneUnsubscribe?.();
		this.#laneUnsubscribe = undefined;
		if (this.#laneSyncTimer !== undefined) clearInterval(this.#laneSyncTimer);
		this.#laneSyncTimer = undefined;
		this.#chatView?.dispose();
		this.#chatView = undefined;
		this.#documentContainer.clear();
		this.#pendingMessagesContainer.clear();
		this.#extensionWidgetsAbove.clear();
		this.#extensionWidgetsBelow.clear();
		this.#statusContainer.clear();
	}

	async #runPrompt(messageText: string): Promise<void> {
		const prompt = messageText.trim();
		if (prompt.length === 0) return;
		if (prompt.startsWith("/")) {
			const separator = prompt.indexOf(" ");
			const name = prompt.slice(1, separator === -1 ? undefined : separator);
			const args = separator === -1 ? "" : prompt.slice(separator + 1).trim();
			await this.#executeSlashCommand(name, args);
			return;
		}
		this.#chatInput.setText("");
		try {
			await this.#submitPrompt(prompt);
		} catch (error) {
			this.#status = `Error: ${message(error)}`;
			this.#rebuild();
		}
	}

	async #executeSlashCommand(name: string, args: string): Promise<void> {
		const command = this.#selectedSlashCommands()
			?.list()
			.find((candidate) => candidate.name === name);
		this.#chatInput.setText("");
		if (command === undefined) {
			this.#status = `Unknown slash command: /${name}`;
			this.#rebuild();
			return;
		}
		try {
			const result = await command.run(args, BACKGROUND_CONTEXT);
			if (result !== undefined) {
				if ("entryId" in result) this.#reportQueue(result);
				else this.#reportOperation(result);
			}
		} catch (error) {
			this.#status = `Error: ${message(error)}`;
			this.#rebuild();
		}
	}

	async #submitPrompt(prompt: string): Promise<void> {
		const controller = this.#selectedController();
		if (controller === undefined) throw new Error("No Session AgentController service is available");
		const operation = this.#laneSnapshot()?.operation;
		const running = operation !== null && operation !== undefined;
		this.#status = running ? "Queueing steering message…" : "Running turn…";
		this.#rebuild();
		if (running) this.#reportQueue(await controller.steer({ message: prompt, images: null }, BACKGROUND_CONTEXT));
		else this.#reportOperation(await controller.prompt({ message: prompt, images: null }, BACKGROUND_CONTEXT));
	}

	async #queueFollowUp(text: string): Promise<void> {
		const controller = this.#selectedController();
		if (controller === undefined) return;
		try {
			this.#status = "Queueing follow-up…";
			this.#rebuild();
			this.#reportQueue(await controller.followUp({ message: text, images: null }, BACKGROUND_CONTEXT));
		} catch (error) {
			this.#status = `Error: ${message(error)}`;
			this.#rebuild();
		}
	}

	#reportOperation(response: AgentOperationResponse): void {
		this.#status = response.accepted
			? response.error === null
				? ""
				: `Operation failed: ${response.error.message}`
			: `Operation rejected: ${response.error.message}`;
		this.#rebuild();
	}

	#reportQueue(response: AgentQueueResponse): void {
		this.#status = response.accepted ? `Queued ${response.entryId}.` : `Message rejected: ${response.error.message}`;
		this.#rebuild();
	}

	#toggleToolsExpanded(): void {
		const previous = this.#toolsExpanded;
		const next = !previous;
		this.#toolsExpanded = next;
		this.#chatView?.setToolsExpanded(next);
		this.#rebuild();

		const commands = this.#extensionCommands;
		if (commands === undefined) return;
		void commands.setToolsExpanded(next, BACKGROUND_CONTEXT).catch((error: unknown) => {
			if (this.#toolsExpanded === next) {
				this.#toolsExpanded = previous;
				this.#chatView?.setToolsExpanded(previous);
			}
			this.#status = `Error: ${message(error)}`;
			this.#rebuild();
		});
	}

	#interrupt(): void {
		const controller = this.#selectedController();
		if (controller === undefined) return;
		this.#status = "Aborting current operation…";
		this.#rebuild();
		void controller.abortCurrent(BACKGROUND_CONTEXT).catch((error: unknown) => {
			this.#status = `Error: ${message(error)}`;
			this.#rebuild();
		});
	}

	#selectedController(): AgentController | undefined {
		return this.#controller;
	}

	#laneSnapshot() {
		const snapshot = this.#session?.transcript.state.value?.snapshot;
		return snapshot === null ? undefined : snapshot;
	}

	#footer(): string {
		const snapshot = this.#laneSnapshot();
		if (!snapshot) return "/model · /thinking · /compact · /reload";
		return `${snapshot.configuration.model.provider}/${snapshot.configuration.model.modelId} · thinking:${snapshot.configuration.thinkingLevel} · ${snapshot.stats.messageCount} messages · /model · /thinking · /compact · /reload`;
	}
}

async function prepareClientSession(
	command: ClientCommand,
	servers: readonly ClientTuiServer[],
): Promise<PreparedClientSession> {
	const opened = servers.map((server) => ({
		server,
		services: server.server.open({
			services: [SessionDirectory, SessionManagement, PresentationPlugins],
			assertAccess() {},
			onError() {},
		}),
	}));
	try {
		const features = opened.map(({ server, services }) => ({
			server,
			directory: services.use(SessionDirectory),
			management: services.use(SessionManagement),
			plugins: services.use(PresentationPlugins),
		}));
		await Promise.all(opened.map(({ services }) => services.ready(BACKGROUND_CONTEXT)));
		let selected:
			| {
					readonly server: ClientTuiServer;
					readonly management: SessionManagement;
					readonly plugins: PresentationPlugins;
					readonly summary: SessionSummary;
			  }
			| undefined;
		if (command.sessionId !== undefined) {
			const matches = features.flatMap((feature) =>
				(feature.directory.state.value?.sessions ?? [])
					.filter((session) => session.sessionId === command.sessionId)
					.map((summary) => ({
						server: feature.server,
						management: feature.management,
						plugins: feature.plugins,
						summary,
					})),
			);
			if (matches.length > 1) throw new Error(`Session ${command.sessionId} is available from more than one server`);
			selected = matches[0];
			if (selected === undefined) {
				if (command.connect?.transport === "radius") {
					throw new Error(`Remote server does not contain Session ${command.sessionId}`);
				}
				const feature = requireSingleServer(features);
				selected = {
					server: feature.server,
					management: feature.management,
					plugins: feature.plugins,
					summary: await feature.management.create(
						{ id: command.sessionId, cwd: process.cwd() },
						BACKGROUND_CONTEXT,
					),
				};
			}
		} else if (command.continue === true || command.resume === true) {
			selected = features
				.flatMap((feature) =>
					(feature.directory.state.value?.sessions ?? []).map((summary) => ({
						server: feature.server,
						management: feature.management,
						plugins: feature.plugins,
						summary,
					})),
				)
				.sort(
					(left, right) =>
						right.summary.createdAt - left.summary.createdAt ||
						left.summary.serverId.localeCompare(right.summary.serverId) ||
						left.summary.sessionId.localeCompare(right.summary.sessionId),
				)[0];
		}
		if (selected === undefined) {
			const feature = requireSingleServer(features);
			selected = {
				server: feature.server,
				management: feature.management,
				plugins: feature.plugins,
				summary: await feature.management.create({ cwd: process.cwd() }, BACKGROUND_CONTEXT),
			};
		}
		const presentationPlugins = await selected.plugins.prepareSession(
			{
				sessionId: selected.summary.sessionId,
				packagePaths: command.pluginPackages?.map((packagePath) => resolve(packagePath)) ?? null,
			},
			BACKGROUND_CONTEXT,
		);
		await selected.management.attach(selected.summary.sessionId, BACKGROUND_CONTEXT);
		await selected.server.session.whenAttached(selected.summary.sessionId, BACKGROUND_CONTEXT);
		return {
			server: selected.server,
			summary: selected.summary,
			presentationPlugins,
		};
	} finally {
		await Promise.allSettled(opened.map(({ services }) => services.dispose(BACKGROUND_CONTEXT)));
	}
}

export async function runClientTui(command: ClientCommand, options: RunClientTuiOptions = {}): Promise<void> {
	const cwd = process.cwd();
	const agentDir = getAgentDir();
	const settingsManager = SettingsManager.create(cwd, agentDir);
	const resourceLoader = new DefaultResourceLoader({
		cwd,
		agentDir,
		settingsManager,
		noExtensions: false,
		noSkills: true,
		noPromptTemplates: true,
		noContextFiles: true,
	});
	await resourceLoader.reload();
	setRegisteredThemes(resourceLoader.getThemes().themes);
	const extensionRenderers: Record<string, ToolRenderers> = {};
	const messageRenderers = new Map<string, MessageRenderer>();
	const entryRenderers = new Map<string, EntryRenderer>();
	for (const extension of resourceLoader.getExtensions().extensions) {
		for (const [customType, renderer] of extension.messageRenderers) messageRenderers.set(customType, renderer);
		for (const [customType, renderer] of extension.entryRenderers ?? []) entryRenderers.set(customType, renderer);
		for (const { definition } of extension.tools.values()) {
			if (definition.renderCall || definition.renderResult || definition.renderShell) {
				extensionRenderers[definition.name] = definition;
			}
		}
	}
	const runtime = await openClientRuntime(command, options);
	const tui = createInteractiveTui({
		tuiMode: "fullscreen",
		showHardwareCursor: settingsManager.getShowHardwareCursor(),
		logDirectory: agentDir,
	});
	tui.setClearOnShrink(settingsManager.getClearOnShrink());
	let component: ExperimentalClientTui | undefined;
	let tuiStarted = false;
	const themeController = new InteractiveThemeController(tui, {
		getSettingsManager: () => settingsManager,
		showError: (error) => component?.showError(error),
		onChanged: () => component?.refreshTheme(),
	});
	try {
		let finish!: () => void;
		const finished = new Promise<void>((resolve) => {
			finish = () => {
				themeController.disableAutoSync();
				if (tuiStarted) {
					tui.stop();
					tuiStarted = false;
				}
				resolve();
			};
		});
		component = await ExperimentalClientTui.create({
			command,
			ui: tui,
			servers: runtime.servers.map((server) => ({
				serverId: server.route.serverId,
				radius: server.route.transport === "radius",
				server: server.server,
				session: server.session,
			})),
			facetLoader: options.facetLoader,
			extensionRenderers,
			messageRenderers,
			entryRenderers,
			requestRender: () => tui.requestRender(),
			finish,
		});
		tui.addChild(component);
		tui.setLayoutRoot(component.layoutRoot);
		tui.setFocus(component);
		tuiStarted = true;
		tui.start();
		await themeController.applyFromSettings();
		await finished;
	} finally {
		themeController.dispose();
		stopThemeWatcher();
		if (tuiStarted) tui.stop();
		await component?.close();
		await runtime.dispose();
	}
}

function requireSingleServer<T>(features: readonly T[]): T {
	if (features.length !== 1) throw new Error("Starting a Session requires exactly one server");
	return features[0]!;
}

function message(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}
