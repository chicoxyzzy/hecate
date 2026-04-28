import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { NewTaskSlideOver } from "./NewTaskSlideOver";

function setup(propOverrides: Partial<React.ComponentProps<typeof NewTaskSlideOver>> = {}) {
  const props: React.ComponentProps<typeof NewTaskSlideOver> = {
    open: true,
    models: [],
    busyAction: "",
    onClose: vi.fn(),
    onCreate: vi.fn(),
    ...propOverrides,
  };
  const user = userEvent.setup();
  return { props, user, render: () => render(<NewTaskSlideOver {...props} />) };
}

describe("NewTaskSlideOver visibility", () => {
  it("renders nothing when open is false", () => {
    const { render } = setup({ open: false });
    const { container } = render();
    expect(container.firstChild).toBeNull();
  });

  it("renders the panel when open is true", () => {
    const { render } = setup();
    render();
    expect(screen.getByText(/new task/i)).toBeTruthy();
  });

  it("starts on the shell tab and shows the shell command field", () => {
    const { render } = setup();
    render();
    expect(screen.getByPlaceholderText(/ls -la/i)).toBeTruthy();
  });
});

describe("NewTaskSlideOver kind switching", () => {
  it("switching to git swaps in the git command field", async () => {
    const { render, user } = setup();
    render();
    await user.click(screen.getByRole("button", { name: "Git" }));
    expect(screen.getByPlaceholderText(/status \/ log/i)).toBeTruthy();
    expect(screen.queryByPlaceholderText(/ls -la/i)).toBeNull();
  });

  it("switching to file shows path and content inputs", async () => {
    const { render, user } = setup();
    render();
    await user.click(screen.getByRole("button", { name: "File" }));
    expect(screen.getByPlaceholderText(/\/path\/to\/file/i)).toBeTruthy();
    expect(screen.getByPlaceholderText(/file content/i)).toBeTruthy();
  });

  it("switching to agent loop shows the prompt textarea", async () => {
    const { render, user } = setup();
    render();
    await user.click(screen.getByRole("button", { name: "Agent loop" }));
    expect(screen.getByPlaceholderText(/describe the task/i)).toBeTruthy();
  });

  it("hides the working-directory input for file kind, shows it for shell/git/agent_loop", async () => {
    const { render, user } = setup();
    render();
    // Default kind shell — working dir is visible.
    expect(screen.getByPlaceholderText(/\(default\)/i)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "File" }));
    // File tasks have their own file_path field, no separate cwd.
    expect(screen.queryByPlaceholderText(/\(default\)/i)).toBeNull();
    // Agent_loop tasks DO show the working-directory input — needed
    // for the "Run in place" toggle (target the operator's real
    // repo). Switching to agent_loop should re-show it.
    await user.click(screen.getByRole("button", { name: "Agent loop" }));
    expect(screen.getByPlaceholderText(/\(default\)/i)).toBeTruthy();
  });

  it("hides the description input on the agent_loop tab (the prompt IS the description)", async () => {
    const { render, user } = setup();
    render();
    await user.click(screen.getByRole("button", { name: "Agent loop" }));
    expect(screen.queryByPlaceholderText(/human-readable description/i)).toBeNull();
  });
});

describe("NewTaskSlideOver submit", () => {
  it("disables 'Queue task' until the required field is filled", async () => {
    const { render, user } = setup();
    render();
    const queueBtn = screen.getByRole("button", { name: /queue task/i }) as HTMLButtonElement;
    expect(queueBtn.disabled).toBe(true);
    await user.type(screen.getByPlaceholderText(/ls -la/i), "echo hi");
    expect(queueBtn.disabled).toBe(false);
  });

  it("submits a shell payload with the trimmed command", async () => {
    const onCreate = vi.fn();
    const { render, user } = setup({ onCreate });
    render();
    await user.type(screen.getByPlaceholderText(/ls -la/i), "echo hi");
    await user.click(screen.getByRole("button", { name: /queue task/i }));
    expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({
      execution_kind: "shell",
      shell_command: "echo hi",
      prompt: "echo hi",
    }));
  });

  it("submits a git payload with `git ${command}` as the fallback prompt", async () => {
    const onCreate = vi.fn();
    const { render, user } = setup({ onCreate });
    render();
    await user.click(screen.getByRole("button", { name: "Git" }));
    await user.type(screen.getByPlaceholderText(/status/i), "log --oneline");
    await user.click(screen.getByRole("button", { name: /queue task/i }));
    expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({
      execution_kind: "git",
      git_command: "log --oneline",
      prompt: "git log --oneline",
    }));
  });

  it("submits a file payload with the chosen operation and content", async () => {
    const onCreate = vi.fn();
    const { render, user } = setup({ onCreate });
    render();
    await user.click(screen.getByRole("button", { name: "File" }));
    await user.type(screen.getByPlaceholderText(/\/path\/to\/file/i), "/tmp/note.txt");
    await user.type(screen.getByPlaceholderText(/file content/i), "hello");
    await user.click(screen.getByRole("button", { name: /queue task/i }));
    expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({
      execution_kind: "file",
      file_path: "/tmp/note.txt",
      file_content: "hello",
      file_operation: "write",
    }));
  });

  it("includes working_directory only when filled", async () => {
    const onCreate = vi.fn();
    const { render, user } = setup({ onCreate });
    render();
    await user.type(screen.getByPlaceholderText(/ls -la/i), "echo hi");
    await user.type(screen.getByPlaceholderText(/\(default\)/i), "/tmp");
    await user.click(screen.getByRole("button", { name: /queue task/i }));
    expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({
      working_directory: "/tmp",
    }));
  });

  it("omits working_directory when blank — the gateway treats absence as 'workspace root'", async () => {
    // Sending an empty string lands as a literal "" working_directory at
    // the gateway, which is not the same as omission. The optional-spread
    // pattern in submit() guards against this; the test asserts it.
    const onCreate = vi.fn();
    const { render, user } = setup({ onCreate });
    render();
    await user.type(screen.getByPlaceholderText(/ls -la/i), "echo hi");
    await user.click(screen.getByRole("button", { name: /queue task/i }));
    const payload = onCreate.mock.calls[0][0];
    expect(payload.working_directory).toBeUndefined();
  });

  it("Enter key in shell command field submits when valid", async () => {
    const onCreate = vi.fn();
    const { render, user } = setup({ onCreate });
    render();
    const input = screen.getByPlaceholderText(/ls -la/i);
    await user.type(input, "echo hi{Enter}");
    expect(onCreate).toHaveBeenCalled();
  });
});

describe("NewTaskSlideOver close behavior", () => {
  it("clicking the X button calls onClose", async () => {
    const onClose = vi.fn();
    const { render, user } = setup({ onClose });
    render();
    // The X is the only ghost button in the header (no accessible name).
    // Find it by walking up from the panel title.
    const buttons = screen.getAllByRole("button");
    // The cancel button has text "Cancel" — exclude it. The X is the first
    // ghost button we encounter.
    const xButton = buttons.find(b => b.querySelector("svg") && !b.textContent?.trim());
    expect(xButton).toBeTruthy();
    await user.click(xButton!);
    expect(onClose).toHaveBeenCalled();
  });

  it("clicking 'Cancel' calls onClose", async () => {
    const onClose = vi.fn();
    const { render, user } = setup({ onClose });
    render();
    await user.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(onClose).toHaveBeenCalled();
  });

  it("clicking the backdrop calls onClose", async () => {
    const onClose = vi.fn();
    const { render, user } = setup({ onClose });
    const { container } = render();
    // The outermost div is the backdrop. Click it directly.
    await user.click(container.firstChild as Element);
    expect(onClose).toHaveBeenCalled();
  });
});

describe("NewTaskSlideOver feedback", () => {
  it("renders the errorMessage prop when provided", () => {
    const { render } = setup({ errorMessage: "rate limited" });
    render();
    expect(screen.getByText(/rate limited/i)).toBeTruthy();
  });

  it("shows the busy label on the queue button while creating", () => {
    const { render } = setup({ busyAction: "create" });
    render();
    expect(screen.getByRole("button", { name: /creating/i })).toBeTruthy();
  });
});
