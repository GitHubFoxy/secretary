import { describe, expect, test } from "vitest";
import { summary } from "../src/extensions/secretary.ts";

describe("Secretary extension live view", () => {
	test("renders only conversation bodies, allowlisted status and tool name/start/finish", () => {
		const text = summary({
			conversation: [{ id: "hidden-id", seq: 1, body: "safe reply", credential: "secret" }],
			workers: [], approvals: [],
			workerActivity: [
				{ seq: 1, kind: "tool_call", tool: "shell", arguments: { command: "private" }, raw: "ACP" },
				{ seq: 2, kind: "tool_result", tool: "shell", output: "private output" },
				{ seq: 3, kind: "status", status: "working", reasoning: "private thought" },
				{ seq: 4, kind: "text", text: "private output" },
			],
			secretaryEvents: [], connection: "connected",
		});
		expect(text).toContain("safe reply");
		expect(text).toContain("Tool started: shell");
		expect(text).toContain("Tool finished: shell");
		expect(text).toContain("Worker status: working");
		for (const forbidden of ["private", "ACP", "secret", "hidden-id", "reasoning"]) expect(text).not.toContain(forbidden);
	});
});
