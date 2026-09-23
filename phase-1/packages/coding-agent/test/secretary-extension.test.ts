import { describe, expect, test } from "vitest";
import { summary, workerPickerOptions } from "../src/extensions/secretary.ts";

describe("Secretary extension live view", () => {
	test("uses Worker titles in the picker and keeps refs internal", () => {
		expect(workerPickerOptions([
			{ worker_ref: "wrk_hidden_a", title: "Pong", status: "idle" },
			{ worker_ref: "wrk_hidden_b", title: "Pong", status: "working" },
			{ worker_ref: "wrk_hidden_c", title: "", status: "idle" },
			{ worker_ref: "wrk_hidden_d", title: "Closed", status: "closed" },
			{ worker_ref: "wrk_hidden_e", title: "Archived", status: "idle", archived: true },
		])).toEqual([
			{ label: "Worker: Pong (1)", workerRef: "wrk_hidden_a" },
			{ label: "Worker: Pong (2)", workerRef: "wrk_hidden_b" },
			{ label: "Worker: wrk_hidden_c", workerRef: "wrk_hidden_c" },
		]);
	});

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
		expect(text).toContain("Secretary server: удалённый чат; ввод ниже пойдёт в локальный Pi");
		expect(text).toContain("Worker status: working");
		expect(text).toContain("Tool started: shell");
		expect(text).toContain("Tool finished: shell");
		for (const forbidden of ["private", "raw output", "hidden-ref", "hidden-event-id", "hidden-status-detail", "secret", "reasoning"]) {
			expect(text).not.toContain(forbidden);
		}
	});
});
