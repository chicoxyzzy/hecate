import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
    getRuntimeStats: vi.fn(),
    getTrace: vi.fn(),
    createTask: vi.fn(),
    startTask: vi.fn(),
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
  getRuntimeStats: apiMocks.getRuntimeStats,
  getTrace: apiMocks.getTrace,
  createTask: apiMocks.createTask,
  startTask: apiMocks.startTask,
  resolveTaskApproval: apiMocks.resolveTaskApproval,
  cancelTaskRun: apiMocks.cancelTaskRun,
  streamTaskRun: apiMocks.streamTaskRun,
}));

describe("RunsView", () => {
  const runtimeStatsResponse = {
    object: "runtime_stats",
    data: {
      checked_at: "2026-01-01T00:00:00Z",
      queue_depth: 1,
      queue_capacity: 128,
      queue_backend: "memory",
      worker_count: 1,
      in_flight_jobs: 0,
      queued_runs: 1,
      running_runs: 0,
      awaiting_approval_runs: 0,
      oldest_queued_age_seconds: 1,
      oldest_running_age_seconds: 0,
      store_backend: "memory",
    },
  };

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
    apiMocks.getRuntimeStats.mockResolvedValue(runtimeStatsResponse);

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

  it("creates and starts a shell task from the composer", async () => {
    apiMocks.getTasks
      .mockResolvedValueOnce({ object: "tasks", data: [] })
      .mockResolvedValueOnce({
        object: "tasks",
        data: [
          {
            id: "task_new",
            title: "List repo",
            prompt: "Inspect the workspace.",
            status: "queued",
            latest_run_id: "run_new",
          },
        ],
      });
    apiMocks.createTask.mockResolvedValue({
      object: "task",
      data: {
        id: "task_new",
        title: "List repo",
        prompt: "Inspect the workspace.",
        status: "queued",
        latest_run_id: "",
      },
    });
    apiMocks.startTask.mockResolvedValue({
      object: "task_run",
      data: {
        id: "run_new",
        task_id: "task_new",
        number: 1,
        status: "awaiting_approval",
      },
    });
    apiMocks.getTask.mockResolvedValue({
      object: "task",
      data: {
        id: "task_new",
        title: "List repo",
        prompt: "Inspect the workspace.",
        status: "awaiting_approval",
        latest_run_id: "run_new",
        pending_approval_count: 1,
      },
    });
    apiMocks.getTaskRuns.mockResolvedValue({
      object: "task_runs",
      data: [
        {
          id: "run_new",
          task_id: "task_new",
          number: 1,
          status: "awaiting_approval",
        },
      ],
    });
    apiMocks.getTaskApprovals.mockResolvedValue({ object: "task_approvals", data: [] });
    apiMocks.getTaskRunSteps.mockResolvedValue({ object: "task_steps", data: [] });
    apiMocks.getTaskRunArtifacts.mockResolvedValue({ object: "task_artifacts", data: [] });
    apiMocks.getRuntimeStats.mockResolvedValue(runtimeStatsResponse);

    render(<RunsView authToken="tenant-secret" session={{ isAuthenticated: true }} />);

    await waitFor(() => expect(screen.getByText("No tasks yet")).toBeInTheDocument());

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "List repo" } });
    fireEvent.change(screen.getByLabelText("Execution kind"), { target: { value: "shell" } });
    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "Inspect the workspace." } });
    fireEvent.change(screen.getByLabelText("Shell command"), { target: { value: "pwd" } });

    fireEvent.click(screen.getByRole("button", { name: "Create and start" }));

    await waitFor(() =>
      expect(apiMocks.createTask).toHaveBeenCalledWith(
        {
          title: "List repo",
          prompt: "Inspect the workspace.",
          execution_kind: "shell",
          shell_command: "pwd",
          git_command: undefined,
          working_directory: undefined,
          file_operation: undefined,
          file_path: undefined,
          file_content: undefined,
        },
        "tenant-secret",
      ),
    );
    await waitFor(() => expect(apiMocks.startTask).toHaveBeenCalledWith("task_new", "tenant-secret"));
    await waitFor(() => expect(screen.getByText("Task created and started.")).toBeInTheDocument());
    expect(screen.getByText("List repo")).toBeInTheDocument();
  });
});
