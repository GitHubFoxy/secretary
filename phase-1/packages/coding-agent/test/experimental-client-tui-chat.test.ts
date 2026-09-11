import type { LaneSnapshot } from "@earendil-works/pi-agent-core";
import { ProcessTerminal, Text, TuiMainScreen } from "@earendil-works/pi-tui";
import { beforeAll, expect, test } from "vitest";
import { ExperimentalChatView } from "../src/experimental/client-tui-chat.ts";
import { initTheme } from "../src/modes/interactive/theme/theme.ts";

beforeAll(() => initTheme("dark"));

function snapshot(): LaneSnapshot {
	return {
		lane: "main",
		transcript: [
			{ id: "entry", parentId: null, seq: 1, timestamp: 1, type: "custom", customType: "card", data: { value: 1 } },
			{
				id: "message",
				parentId: "entry",
				seq: 2,
				timestamp: 2,
				type: "message",
				message: { role: "custom", customType: "notice", content: "hello", display: true, timestamp: 2 },
			},
			{
				id: "assistant",
				parentId: "message",
				seq: 3,
				timestamp: 3,
				type: "message",
				message: {
					role: "assistant",
					content: [{ type: "toolCall", id: "tool-1", name: "fixture", arguments: {} }],
					provider: "test",
					model: "test",
					api: "test",
					stopReason: "toolUse",
					timestamp: 3,
					usage: {
						input: 0,
						output: 0,
						cacheRead: 0,
						cacheWrite: 0,
						totalTokens: 0,
						cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
					},
				},
			},
			{
				id: "result",
				parentId: "assistant",
				seq: 4,
				timestamp: 4,
				type: "message",
				message: {
					role: "toolResult",
					toolCallId: "tool-1",
					toolName: "fixture",
					content: [{ type: "text", text: "raw" }],
					isError: false,
					timestamp: 4,
				},
			},
		],
		tipId: "result",
		configuration: { model: { provider: "test", modelId: "test" }, thinkingLevel: "off", activeToolNames: [] },
		stats: {
			messageCount: 4,
			usage: {
				input: 0,
				output: 0,
				cacheRead: 0,
				cacheWrite: 0,
				totalTokens: 0,
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
			},
		},
		operation: null,
		queues: [],
		faulted: false,
	};
}

test("uses classic extension renderers for tools, messages, and entries", () => {
	const view = new ExperimentalChatView(
		new TuiMainScreen(new ProcessTerminal()),
		"/tmp",
		{
			fixture: {
				renderCall: () => new Text("TOOL_RENDERER", 0, 0),
				renderResult: () => new Text("RESULT_RENDERER", 0, 0),
			},
		},
		new Map([["notice", () => new Text("MESSAGE_RENDERER", 0, 0)]]),
		new Map([["card", () => new Text("ENTRY_RENDERER", 0, 0)]]),
	);
	view.apply(snapshot());
	const rendered = view.transcript.render(80).join("\n");
	expect(rendered).toContain("ENTRY_RENDERER");
	expect(rendered).toContain("MESSAGE_RENDERER");
	expect(rendered).toContain("TOOL_RENDERER");
	expect(rendered).toContain("RESULT_RENDERER");
	view.dispose();
});
