import { describe, expect, test } from "vitest";
import { summary } from "../src/extensions/secretary.ts";

describe("Secretary extension live view", () => {
	test("renders allowlisted Worker status and tool event envelope fields only", () => {
		const text = summary({
			conversation: [{ id: "hidden-id", seq: 1, body: "safe reply", credential: "secret" }],
			workers: [],
			approvals: [],
			selectedWorker: { worker: { worker_ref: "hidden-ref", status: "working", reasoning: "hidden-status-detail" }, turns: [] },
			workerActivity: [
				{
					id: "hidden-event-id", seq: 1, kind: "attempt.activity",
					payload: { kind: "tool_call", tool: "shell", arguments: { command: "private argument", reasoning: "private thought" } },
				},
				{
					id: "hidden-event-id-2", seq: 2, kind: "attempt.activity",
					payload: { kind: "tool_result", tool: "shell", result: "private output", reasoning: "private thought" },
				},
				{
					seq: 3, kind: "attempt.activity",
					payload: { kind: "text", tool: "not-a-tool-event", text: "raw output" },
				},
			],
			secretaryEvents: [],
			connection: "connected",
		});
		expect(text).toContain("safe reply");
		expect(text).toContain("Worker status: working");
		expect(text).toContain("Tool started: shell");
		expect(text).toContain("Tool finished: shell");
		for (const forbidden of ["private", "raw output", "hidden-ref", "hidden-event-id", "hidden-status-detail", "secret", "reasoning"]) {
			expect(text).not.toContain(forbidden);
		}
	});
});
