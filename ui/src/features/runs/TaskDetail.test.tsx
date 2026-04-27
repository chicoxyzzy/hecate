import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { TaskDetail } from "./TaskDetail";
import type { TaskRecord, TaskRunRecord, TaskStepRecord } from "../../types/runtime";

function makeTask(overrides: Partial<TaskRecord> = {}): TaskRecord {
  return {
    id: "task-1",
    title: "List the working directory",
    prompt: "ls -la",
    status: "completed",
    execution_kind: "shell",
    shell_command: "ls -la",
    step_count: 2,
    latest_run_id: "run-1",
    ...overrides,
  } as TaskRecord;
}

function makeRun(overrides: Partial<TaskRunRecord> = {}): TaskRunRecord {
  return {
    id: "run-1",
    task_id: "task-1",
    number: 1,
    status: "completed",
    model: "gpt-4o-mini",
    started_at: "2026-04-27T17:00:00Z",
    finished_at: "2026-04-27T17:00:02Z",
    ...overrides,
  } as TaskRunRecord;
}

function makeStep(overrides: Partial<TaskStepRecord> = {}): TaskStepRecord {
  return {
    id: "step-1",
    task_id: "task-1",
    run_id: "run-1",
    index: 0,
    kind: "shell",
    title: "ls -la",
    status: "completed",
    started_at: "2026-04-27T17:00:00Z",
    finished_at: "2026-04-27T17:00:01Z",
    exit_code: 0,
    ...overrides,
  } as TaskStepRecord;
}

function setup(propOverrides: Partial<React.ComponentProps<typeof TaskDetail>> = {}) {
  const task = makeTask();
  const run = makeRun();
  const props: React.ComponentProps<typeof TaskDetail> = {
    task,
    run,
    runs: [run],
    selectedRunID: run.id,
    steps: [],
    artifacts: [],
    approvals: [],
    streamState: "closed",
    busyAction: "",
    notice: null,
    onSelectRun: vi.fn(),
    onResolveApproval: vi.fn(),
    onCancelRun: vi.fn(),
    onRetryRun: vi.fn(),
    onResumeRun: vi.fn(),
    onRetryFromTurn: vi.fn(),
    ...propOverrides,
  };
  const user = userEvent.setup();
  return { props, user, render: () => render(<TaskDetail {...props} />) };
}

describe("TaskDetail run picker", () => {
  it("shows the current run number", () => {
    const { render } = setup();
    render();
    expect(screen.getByRole("button", { name: /select run/i })).toHaveTextContent("run #1");
  });

  it("renders 'of N' suffix only when there are multiple runs", () => {
    const run1 = makeRun({ id: "run-1", number: 1 });
    const run2 = makeRun({ id: "run-2", number: 2, status: "failed" });
    const { render } = setup({ runs: [run2, run1], run: run2, selectedRunID: run2.id });
    render();
    expect(screen.getByRole("button", { name: /select run/i })).toHaveTextContent("of 2");
  });

  it("opens the listbox and shows all runs when clicked", async () => {
    const run1 = makeRun({ id: "run-1", number: 1, status: "failed" });
    const run2 = makeRun({ id: "run-2", number: 2, status: "completed" });
    const { render, user } = setup({ runs: [run2, run1], run: run2, selectedRunID: run2.id });
    render();
    await user.click(screen.getByRole("button", { name: /select run/i }));
    const listbox = await screen.findByRole("listbox");
    expect(listbox).toBeTruthy();
    expect(screen.getAllByRole("option")).toHaveLength(2);
  });

  it("calls onSelectRun with the chosen run id", async () => {
    const onSelectRun = vi.fn();
    const run1 = makeRun({ id: "run-1", number: 1, status: "failed" });
    const run2 = makeRun({ id: "run-2", number: 2, status: "completed" });
    const { render, user } = setup({ runs: [run2, run1], run: run2, selectedRunID: run2.id, onSelectRun });
    render();
    await user.click(screen.getByRole("button", { name: /select run/i }));
    const options = await screen.findAllByRole("option");
    await user.click(options[1]); // run-1
    expect(onSelectRun).toHaveBeenCalledWith("run-1");
  });

  it("hides the picker when there are zero runs", () => {
    const { render } = setup({ runs: [], run: null });
    render();
    expect(screen.queryByRole("button", { name: /select run/i })).toBeNull();
  });
});

describe("TaskDetail step drill-down", () => {
  it("renders a step row with the title", () => {
    const step = makeStep({ title: "echo hello" });
    const { render } = setup({ steps: [step] });
    render();
    expect(screen.getByText("echo hello")).toBeTruthy();
  });

  it("clicking a step with detail toggles the expanded panel", async () => {
    const step = makeStep({
      title: "echo hello",
      tool_name: "shell",
      input: { command: "echo hello" },
      output_summary: { exit_code: 0, stdout_size: 6 },
    });
    const { render, user } = setup({ steps: [step] });
    render();

    expect(screen.queryByText(/^INPUT$/i)).toBeNull();

    await user.click(screen.getByRole("button", { name: /step echo hello/i }));
    expect(await screen.findByText(/^INPUT$/i)).toBeTruthy();
    expect(screen.getByText(/^OUTPUT$/i)).toBeTruthy();
    expect(screen.getByText(/"command"/)).toBeTruthy();
  });

  it("shows the error block when a step failed", async () => {
    const step = makeStep({
      title: "rm",
      status: "failed",
      exit_code: 2,
      error: "permission denied",
      input: { command: "rm /etc/passwd" },
    });
    const { render, user } = setup({ steps: [step] });
    render();
    await user.click(screen.getByRole("button", { name: /step rm/i }));
    expect(await screen.findByText(/^Error$/i)).toBeTruthy();
    // Error appears both as inline truncated tooltip and in the expanded
    // panel — use getAllByText and assert at least one occurrence renders.
    expect(screen.getAllByText(/permission denied/i).length).toBeGreaterThan(0);
  });

  it("does not make the step clickable when there is no detail to show", () => {
    const step = makeStep({
      tool_name: undefined,
      phase: undefined,
      input: undefined,
      output_summary: undefined,
      error: undefined,
    });
    const { render } = setup({ steps: [step] });
    render();
    const button = screen.getByRole("button", { name: /step/i });
    // The chevron is only rendered when hasDetail; assert no chevron path
    // shows up by checking the button does not contain an aria-expanded toggle effect.
    expect(button.getAttribute("aria-expanded")).toBe("false");
  });
});

describe("TaskDetail agent conversation viewer", () => {
  const conversation = JSON.stringify([
    { role: "user", content: "Summarize the README." },
    {
      role: "assistant",
      content: "Let me read it.",
      tool_calls: [{
        id: "call-1",
        type: "function",
        function: { name: "read_file", arguments: '{"path":"README.md"}' },
      }],
    },
    {
      role: "tool",
      content: "path=README.md size=42 bytes=42\n--- content ---\nHecate is the gateway.",
      tool_call_id: "call-1",
    },
    { role: "assistant", content: "It introduces Hecate as the gateway." },
  ]);

  function makeConvoArtifact(content = conversation) {
    return {
      id: "convo-run-1",
      task_id: "task-1",
      run_id: "run-1",
      kind: "agent_conversation",
      name: "agent-conversation.json",
      content_text: content,
      mime_type: "application/json",
    } as any;
  }

  it("renders the conversation when an agent_conversation artifact is present", () => {
    const { render } = setup({ artifacts: [makeConvoArtifact()] });
    render();
    expect(screen.getByText(/Agent conversation · 4 messages/)).toBeTruthy();
    expect(screen.getByText("Summarize the README.")).toBeTruthy();
    expect(screen.getByText("Let me read it.")).toBeTruthy();
    expect(screen.getByText(/Hecate is the gateway/)).toBeTruthy();
    expect(screen.getByText("It introduces Hecate as the gateway.")).toBeTruthy();
  });

  it("renders tool calls as chips with the function name", () => {
    const { render } = setup({ artifacts: [makeConvoArtifact()] });
    render();
    // Tool-call chip uses an arrow + function name to read fluent —
    // "→ read_file" — and includes the args inline.
    expect(screen.getByText(/→ read_file/)).toBeTruthy();
    expect(screen.getByText(/"path":"README\.md"/)).toBeTruthy();
  });

  it("does NOT render the agent_conversation as a bottom-strip badge (it's inline)", () => {
    const { render } = setup({
      artifacts: [
        makeConvoArtifact(),
        {
          id: "art-2",
          task_id: "task-1",
          run_id: "run-1",
          kind: "summary",
          name: "agent-final-answer.txt",
          content_text: "answer",
        } as any,
      ],
    });
    render();
    // The summary artifact still shows as a chip in the bottom strip.
    expect(screen.getByText("agent-final-answer.txt")).toBeTruthy();
    // The conversation artifact's filename ("agent-conversation.json")
    // must NOT appear in the bottom strip — it's already inline above.
    expect(screen.queryByText("agent-conversation.json")).toBeNull();
  });

  it("falls back to an inline error on corrupt JSON instead of crashing", () => {
    const { render } = setup({ artifacts: [makeConvoArtifact("not valid json {")] });
    render();
    expect(screen.getByText(/Could not parse agent conversation/i)).toBeTruthy();
  });

  it("renders nothing when no agent_conversation artifact exists", () => {
    const { render } = setup({ artifacts: [] });
    render();
    expect(screen.queryByText(/Agent conversation/)).toBeNull();
  });

  it("shows a 'retry from here' button on each assistant turn for terminal runs", async () => {
    const { props, user, render } = setup({
      artifacts: [makeConvoArtifact()],
      run: makeRun({ status: "completed" }),
    });
    render();
    // Two assistant turns in the fixture — both should show the
    // retry control. Click the first one and verify the callback
    // fires with turn=1.
    const retryButtons = screen.getAllByRole("button", { name: /retry from here/i });
    expect(retryButtons.length).toBe(2);
    await user.click(retryButtons[0]);
    expect(props.onRetryFromTurn).toHaveBeenCalledWith(1);
  });

  it("hides the 'retry from here' button while the run is still active", () => {
    const { render } = setup({
      artifacts: [makeConvoArtifact()],
      run: makeRun({ status: "running" }),
    });
    render();
    expect(screen.queryAllByRole("button", { name: /retry from here/i })).toHaveLength(0);
  });

  it("disables 'retry from here' while another action is in flight", () => {
    const { render } = setup({
      artifacts: [makeConvoArtifact()],
      run: makeRun({ status: "completed" }),
      busyAction: "cancel",
    });
    render();
    const retryButtons = screen.getAllByRole("button", { name: /retry from here/i });
    expect(retryButtons.length).toBe(2);
    retryButtons.forEach(b => expect((b as HTMLButtonElement).disabled).toBe(true));
  });
});

describe("TaskDetail run cost badge", () => {
  it("shows just this run's cost when there's no prior chain", () => {
    const { render } = setup({
      run: makeRun({ total_cost_micros_usd: 12_345 }),
    });
    render();
    // 12_345 µUSD ≈ $0.012; toFixed(3) = $0.012
    expect(screen.getByText(/\$0\.012/)).toBeTruthy();
    // No "/ task" suffix when prior chain is empty.
    expect(screen.queryByText(/task/)).toBeNull();
  });

  it("shows cumulative task cost when prior chain has spend", () => {
    const { render } = setup({
      run: makeRun({ total_cost_micros_usd: 250_000, prior_cost_micros_usd: 750_000 }),
    });
    render();
    // This run = $0.250, total = $1.000, with " / $1.000 task" suffix.
    expect(screen.getByText(/\$0\.250/)).toBeTruthy();
    expect(screen.getByText(/\$1\.000 task/)).toBeTruthy();
  });

  it("hides the badge entirely when both costs are zero", () => {
    const { render } = setup({
      run: makeRun({ total_cost_micros_usd: 0, prior_cost_micros_usd: 0 }),
    });
    render();
    // No "$" character anywhere from the badge — guards against an
    // empty $0.000 stub being rendered as visual noise.
    expect(screen.queryByText(/\$0\.000/)).toBeNull();
  });
});
