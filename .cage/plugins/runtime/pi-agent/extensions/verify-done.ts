import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

/**
 * Cursor-like verify gate: if the agent edited code and stopped without
 * running tests / an evidence table, inject a follow-up turn automatically.
 */
export default function (pi: ExtensionAPI) {
	let edited = false;
	let tested = false;
	let nudges = 0;

	const reset = () => {
		edited = false;
		tested = false;
		nudges = 0;
	};

	pi.on("before_agent_start", async (event) => {
		reset();
		const prompt = event.prompt ?? "";
		if (!looksLikeImplementation(prompt)) {
			return;
		}
		return {
			systemPrompt:
				event.systemPrompt +
				"\n\n[verify-done] This is implementation work. Map → implement → run tests → evidence table before finishing. Do not claim done without Validation output.",
		};
	});

	pi.on("tool_call", async (event) => {
		const name = event.toolName ?? "";
		if (name === "write" || name === "edit") {
			edited = true;
		}
		if (name === "bash" || name === "powershell") {
			const command = String((event.input as { command?: string })?.command ?? "");
			if (/\bgo\s+test\b/.test(command) || /implement-pr\/scripts\/verify\.sh/.test(command)) {
				tested = true;
			}
		}
	});

	pi.on("agent_end", async (event, ctx) => {
		if (!edited || nudges >= 2) {
			return;
		}
		const text = lastAssistantText(event.messages);
		const hasEvidence = /Evidence/i.test(text) && /Validation/i.test(text);
		if (tested && hasEvidence) {
			return;
		}
		nudges++;
		const missing = [
			!tested ? "run tests for packages you changed (default: go test ./...)" : "",
			!hasEvidence ? "end with Evidence + Validation sections" : "",
		]
			.filter(Boolean)
			.join("; ");
		ctx.ui.notify(`verify-done: continuing — ${missing}`, "warning");
		pi.sendUserMessage(
			`Automatic verify gate: you edited code but did not finish verification (${missing}). Continue: run the tests, fix failures, then reply with Status/Evidence/Validation. Do not stop with a summary-only done.`,
			{ deliverAs: "followUp" },
		);
	});
}

function looksLikeImplementation(prompt: string): boolean {
	return /\b(implement|fix|refactor|add|create|build|PR|pull request|Tests:|go test|API|endpoint|route)\b/i.test(
		prompt,
	);
}

function lastAssistantText(messages: unknown): string {
	if (!Array.isArray(messages)) {
		return "";
	}
	for (let i = messages.length - 1; i >= 0; i--) {
		const m = messages[i] as { role?: string; content?: unknown };
		if (m?.role !== "assistant") {
			continue;
		}
		if (typeof m.content === "string") {
			return m.content;
		}
		if (Array.isArray(m.content)) {
			return m.content
				.map((part) => {
					if (typeof part === "string") {
						return part;
					}
					const p = part as { type?: string; text?: string };
					return p?.type === "text" ? (p.text ?? "") : "";
				})
				.join("\n");
		}
	}
	return "";
}
