import { fireEvent, render, screen } from "@testing-library/react";
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

  it("calls setMessage as user types", () => {
    const setMessage = vi.fn();
    const { state, actions } = setup({}, { setMessage });
    render(<ChatView state={state} actions={actions} />);
    const ta = screen.getByPlaceholderText(/Message/i) as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: "typed text" } });
    expect(setMessage).toHaveBeenCalledWith("typed text");
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

  it("calls selectChatSession when clicking a session row", () => {
    const selectChatSession = vi.fn(async () => undefined);
    const { state, actions } = setup({
      chatSessions: [{ id: "s1", title: "Pick me", turn_count: 0 } as any],
    }, { selectChatSession });
    render(<ChatView state={state} actions={actions} />);
    fireEvent.click(screen.getByText("Pick me"));
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
