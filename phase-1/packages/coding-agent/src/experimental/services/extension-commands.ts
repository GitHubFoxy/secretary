import { type Context, defineService, type JsonValue, type ReplicatedState } from "@earendil-works/chord";

export interface ExtensionCommandSummary {
	readonly name: string;
	readonly description?: string;
}

/** Commands registered by classic Pi extensions inside the owning Session worker. */
export interface ExtensionCommandCompletion {
	readonly value: string;
	readonly label: string;
	readonly description?: string;
}

export type ExtensionUIPrompt =
	| { readonly id: string; readonly kind: "select" | "confirm"; readonly title: string; readonly options: string[] }
	| {
			readonly id: string;
			readonly kind: "input" | "editor";
			readonly title: string;
			readonly placeholder?: string;
			readonly initial?: string;
	  }
	| { readonly id: string; readonly kind: "custom"; readonly lines: string[]; readonly overlay: boolean };

export interface ExtensionUIWidget {
	readonly content: string[];
	readonly placement: "aboveEditor" | "belowEditor";
}

export interface ExtensionUIEditor {
	readonly lines: string[];
}

export interface ExtensionUIEditorSubmit {
	readonly id: string;
	readonly text: string;
}

export interface ExtensionUIState {
	readonly revision: number;
	readonly prompt: ExtensionUIPrompt | null;
	readonly statuses: Record<string, string>;
	readonly notifications: readonly { id: number; message: string; type: "info" | "warning" | "error" }[];
	readonly terminalInput?: boolean;
	readonly widgets: Record<string, ExtensionUIWidget>;
	readonly editor?: ExtensionUIEditor;
	readonly editorSubmit?: ExtensionUIEditorSubmit;
	readonly workingMessage?: string;
	readonly workingVisible: boolean;
	readonly workingIndicator?: { frames?: string[]; intervalMs?: number };
	readonly hiddenThinkingLabel?: string;
	readonly toolsExpanded?: boolean;
	readonly title?: string;
	readonly footer?: string[];
	readonly header?: string[];
}

export interface ExtensionCommandProvider {
	list(context: Context): Promise<readonly ExtensionCommandSummary[]>;
	complete(name: string, prefix: string, context: Context): Promise<readonly ExtensionCommandCompletion[] | null>;
	run(name: string, args: string, context: Context): Promise<readonly string[]>;
}

export interface ExtensionAutocompleteItem {
	readonly value: string;
	readonly label: string;
	readonly description?: string;
}

export interface ExtensionAutocompletePosition {
	readonly lines: string[];
	readonly cursorLine: number;
	readonly cursorCol: number;
}

export interface ExtensionAutocompleteResult {
	readonly lines: string[];
	readonly cursorLine: number;
	readonly cursorCol: number;
}

export interface ExtensionAutocompleteRequest extends ExtensionAutocompletePosition {
	readonly force?: boolean;
	readonly baseSuggestions: {
		readonly items: readonly ExtensionAutocompleteItem[];
		readonly prefix: string;
	} | null;
	readonly baseCompletions: Record<string, ExtensionAutocompleteResult>;
	readonly baseShouldTriggerFileCompletion?: boolean;
}

export interface ExtensionAutocompleteResponse {
	readonly suggestions: {
		readonly items: readonly ExtensionAutocompleteItem[];
		readonly prefix: string;
	} | null;
	readonly completions: Record<string, ExtensionAutocompleteResult>;
	readonly shouldTriggerFileCompletion?: boolean;
}

export interface ExtensionCommands extends ExtensionCommandProvider {
	readonly uiState: ReplicatedState<ExtensionUIState>;
	respondUI(id: string, value: JsonValue, context: Context): Promise<void>;
	inputUI(id: string, data: string, context: Context): Promise<boolean>;
	autocomplete(request: ExtensionAutocompleteRequest, context: Context): Promise<ExtensionAutocompleteResponse>;
	getAutocompleteTriggerCharacters(context: Context): Promise<readonly string[]>;
	setToolsExpanded(expanded: boolean, context: Context): Promise<void>;
}

export const ExtensionCommands = defineService<ExtensionCommands>("pi.extension-commands");
