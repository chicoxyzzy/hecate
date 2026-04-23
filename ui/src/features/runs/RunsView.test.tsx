import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RunsView } from "./RunsView";

const apiMocks = vi.hoisted(() => {
  let streamListener: ((event: { event: string; payload: any }) => void) | null = null;
  return {
    getTasks: vi.fn(),
    getTask: vi.fn(),
    getTaskRuns: vi.fn(),
    getTaskApprovals: vi.fn(),
    getTaskRunSteps: vi.fn(),
    getTaskRunArtifacts: vi.fn(),
    resolveTaskApproval: vi.fn(),
    cancelTaskRun: vi.fn(),
    streamTaskRun: vi.fn(async (_taskID, _runID, _authToken, onEvent) => {
      streamListener = onEvent;
      await new Promise<void>(() => undefined);
    }),
    emitStreamEvent(event: { event: string; payload: any }) {
      if (streamListener) {
        streamListener(event);
      }
    },
    reset() {
      streamListener = null;
    },
  };
});

vi.mock("../../lib/api", () => ({
  getTasks: apiMocks.getTasks,
  getTask: apiMocks.getTask,
  getTaskRuns: apiMocks.getTaskRuns,
  getTaskApprovals: apiMocks.getTaskApprovals,
  getTaskRunSteps: apiMocks.getTaskRunSteps,
  getTaskRunArtifacts: apiMocks.getTaskRunArtifacts,
  resolveTaskApproval: apiMocks.resolveTaskApproval,
  cancelTaskRun: apiMocks.cancelTaskRun,
  streamTaskRun: apiMocks.streamTaskRun,
}));

describe("RunsView", () => {
  afterEach(() => {
    vi.clearAllMocks();
    apiMocks.reset();
  });

  it("renders authenticated live run data and updates from stream snapshots", async () => {
    apiMocks.getTasks.mockResolvedValue({
      object: "tasks",
      data: [
        {
          id: "task_1",
          title: "Run shell",
          prompt: "Run a shell command.",
          status: "awaiting_approval",
          latest_run_id: "run_1",
          pending_approval_count: 1,
        },
      ],
    });
    apiMocks.getTask.mockResolvedValue({
      object: "task",
      data: {
        id: "task_1",
        title: "Run shell",
        prompt: "Run a shell command.",
        status: "awaiting_approval",
        latest_run_id: "run_1",
        pending_approval_count: 1,
      },
    });
    apiMocks.getTaskRuns.mockResolvedValue({
      object: "task_runs",
      data: [
        {
          id: "run_1",
          task_id: "task_1",
          number: 1,
          status: "awaiting_approval",
          request_id: "req_1",
          trace_id: "trace_1",
          step_count: 0,
          artifact_count: 0,
        },
      ],
    });
    apiMocks.getTaskApprovals.mockResolvedValue({
      object: "task_approvals",
      data: [
        {
          id: "approval_1",
          task_id: "task_1",
          run_id: "run_1",
          kind: "shell_command",
          status: "pending",
          reason: "Shell commands require approval before execution.",
        },
      ],
    });
    apiMocks.getTaskRunSteps.mockResolvedValue({ object: "task_steps", data: [] });
    apiMocks.getTaskRunArtifacts.mockResolvedValue({ object: "task_artifacts", data: [] });

    render(<RunsView authToken="tenant-secret" session={{ isAuthenticated: true }} />);

    await waitFor(() => expect(screen.getByText("Run shell")).toBeInTheDocument());
    expect(screen.getByText("1 pending approval")).toBeInTheDocument();
    expect(screen.getAllByText("awaiting_approval").length).toBeGreaterThan(0);

    act(() => {
      apiMocks.emitStreamEvent({
        event: "snapshot",
        payload: {
          object: "task_run_stream_event",
          data: {
            sequence: 1,
            run: {
              id: "run_1",
              task_id: "task_1",
              number: 1,
              status: "running",
              request_id: "req_1",
              trace_id: "trace_1",
              step_count: 1,
              artifact_count: 2,
            },
            steps: [
              {
                id: "step_1",
                task_id: "task_1",
                run_id: "run_1",
                index: 1,
                kind: "shell",
                title: "Shell command",
                status: "running",
                tool_name: "shell",
              },
            ],
            artifacts: [
              {
                id: "artifact_stdout",
                task_id: "task_1",
                run_id: "run_1",
                step_id: "step_1",
                kind: "stdout",
                status: "streaming",
                content_text: "hello ",
              },
              {
                id: "artifact_stderr",
                task_id: "task_1",
                run_id: "run_1",
                step_id: "step_1",
                kind: "stderr",
                status: "streaming",
                content_text: "",
              },
            ],
          },
        },
      });
    });

    await waitFor(() => expect(screen.getByText("Stream live")).toBeInTheDocument());
    expect(screen.getAllByText("running").length).toBeGreaterThan(0);
    expect(screen.getByText(/hello/)).toBeInTheDocument();
    expect(screen.getByText("Shell command")).toBeInTheDocument();
  });

  it("shows an auth gate when unauthenticated", () => {
    render(<RunsView authToken="" session={{ isAuthenticated: false }} />);

    expect(screen.getByText("Authentication required")).toBeInTheDocument();
    expect(screen.getByText(/Add a bearer token in Access/)).toBeInTheDocument();
  });
});
