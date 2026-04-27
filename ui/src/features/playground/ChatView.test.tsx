import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ChatView } from "./ChatView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

function setup(stateOverrides = {}, actionOverrides = {}) {
  const state = createRuntimeConsoleFixture({
    session: {
      kind: "tenant", label: "Tenant", role: "tenant", isAdmin: false, isAuthenticated: true,
      capabilities: [], name: "ci", tenant: "team-a", source: "config", keyID: "k1",
      allowedProviders: [], allowedModels: [],
    },
    providerScopedModels: [
      { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } },
    ],
    ...stateOverrides,
  });
  const actions = { ...createRuntimeConsoleActions(), ...actionOverrides };
  return { state, actions };
}

describe("ChatView input", () => {
  it("disables the send button when message is empty", () => {
    const { state, actions } = setup({ message: "" });
    render(<ChatView state={state} actions={actions} />);
    const send = document.querySelector("button[type='submit']") as HTMLButtonElement;
    expect(send.disabled).toBe(true);
  });

  it("enables the send button when message has content", () => {
    const { state, actions } = setup({ message: "hello" });
    render(<ChatView state={state} actions={actions} />);
    const send = document.querySelector("button[type='submit']") as HTMLButtonElement;
    expect(send.disabled).toBe(false);
  });

  it("calls setMessage as user types", async () => {
    const setMessage = vi.fn();
    // Start with empty message so the assertion sees only what we typed.
    const { state, actions } = setup({ message: "" }, { setMessage });
    render(<ChatView state={state} actions={actions} />);
    const ta = screen.getByPlaceholderText(/Message/i) as HTMLTextAreaElement;
    const user = userEvent.setup();
    await user.type(ta, "h");
    expect(setMessage).toHaveBeenCalledWith("h");
  });
});

describe("ChatView Enter switch", () => {
  it("renders the segmented Enter/⌘+Enter or Ctrl+Enter switch", () => {
    const { state, actions } = setup();
    render(<ChatView state={state} actions={actions} />);
    // The switch is one of the toggle buttons in the input toolbar.
    const buttons = screen.getAllByRole("button");
    const labels = buttons.map(b => b.textContent?.trim()).filter(Boolean);
    const hasEnterToggle = labels.some(l => l === "↵ to send" || /[⌘+|Ctrl\+]\+?↵ to send/.test(l!));
    expect(hasEnterToggle).toBe(true);
  });
});

describe("ChatView sessions sidebar", () => {
  it("shows 'No sessions yet' when chatSessions is empty", () => {
    const { state, actions } = setup({ chatSessions: [] });
    render(<ChatView state={state} actions={actions} />);
    expect(screen.getByText(/No sessions yet/i)).toBeTruthy();
  });

  it("renders one row per session with title", () => {
    const { state, actions } = setup({
      chatSessions: [
        { id: "s1", title: "First chat", turn_count: 2, updated_at: "2026-04-25T00:00:00Z" } as any,
        { id: "s2", title: "Second chat", turn_count: 1, updated_at: "2026-04-25T01:00:00Z" } as any,
      ],
    });
    render(<ChatView state={state} actions={actions} />);
    expect(screen.getByText("First chat")).toBeTruthy();
    expect(screen.getByText("Second chat")).toBeTruthy();
  });

  it("calls selectChatSession when clicking a session row", async () => {
    const selectChatSession = vi.fn(async () => undefined);
    const { state, actions } = setup({
      chatSessions: [{ id: "s1", title: "Pick me", turn_count: 0 } as any],
    }, { selectChatSession });
    render(<ChatView state={state} actions={actions} />);
    const user = userEvent.setup();
    await user.click(screen.getByText("Pick me"));
    expect(selectChatSession).toHaveBeenCalledWith("s1");
  });
});

describe("ChatView error display", () => {
  it("renders chatError using InlineError styling", () => {
    const { state, actions } = setup({ chatError: "Provider returned 500" });
    render(<ChatView state={state} actions={actions} />);
    expect(screen.getByText(/Provider returned 500/)).toBeTruthy();
  });
});

describe("ChatView session title", () => {
  it("shows 'New conversation' when no sessions and no active session", () => {
    const { state, actions } = setup({ chatSessions: [], activeChatSession: null });
    render(<ChatView state={state} actions={actions} />);
    expect(screen.getByText("New conversation")).toBeTruthy();
  });

  it("shows the active session's title", () => {
    const { state, actions } = setup({
      activeChatSession: { id: "s1", title: "Hello world", turns: [] } as any,
    });
    render(<ChatView state={state} actions={actions} />);
    expect(screen.getByText("Hello world")).toBeTruthy();
  });
});

describe("ChatView New session button", () => {
  it("focuses the message textarea after clicking New session", async () => {
    // The button starts a fresh conversation; the operator's next move
    // is almost always to type. Auto-focusing the textarea saves a
    // click and matches the muscle-memory pattern from chat clients.
    const createChatSession = vi.fn();
    const { state, actions } = setup({}, { createChatSession });
    const user = userEvent.setup();
    render(<ChatView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: /new session/i }));
    expect(createChatSession).toHaveBeenCalled();
    const textarea = screen.getByPlaceholderText(/^Message…/i);
    expect(document.activeElement).toBe(textarea);
  });
});

describe("ChatView session focus", () => {
  it("focuses the message textarea when activeChatSessionID changes (session switch)", async () => {
    // Switching sessions via the sidebar drives activeChatSessionID
    // change → useEffect → focus. Pinning here so a refactor of the
    // mount/scroll effect doesn't accidentally drop the focus call.
    const { state, actions } = setup({ activeChatSessionID: "" });
    const { rerender } = render(<ChatView state={state} actions={actions} />);
    // Move focus to a sibling control so we can detect the focus jump.
    // The Close-sidebar button has a stable accessible name and is
    // outside the textarea.
    const closeBtn = screen.getByTitle("Close");
    closeBtn.focus();
    expect(document.activeElement).toBe(closeBtn);
    // Now simulate the session switch via prop change.
    const next = { ...state, activeChatSessionID: "s1" };
    rerender(<ChatView state={next} actions={actions} />);
    const textarea = screen.getByPlaceholderText(/^Message…/i);
    expect(document.activeElement).toBe(textarea);
  });
});
