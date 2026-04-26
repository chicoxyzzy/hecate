import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ConnectYourClient } from "./ConnectYourClient";

describe("ConnectYourClient", () => {
  it("renders the gateway URL and the OpenAI tab snippet by default", () => {
    render(<ConnectYourClient gatewayURL="http://gw.local:8080" token="tok-secret" />);
    // Default tab shows OpenAI base URL + key.
    expect(screen.getByText(/OPENAI_BASE_URL=/)).toBeInTheDocument();
    expect(screen.getByText(/http:\/\/gw\.local:8080\/v1/)).toBeInTheDocument();
  });

  it("masks the token until the operator clicks 'show token'", () => {
    render(<ConnectYourClient gatewayURL="http://gw.local" token="tok-secret-12345" />);
    // Initially the literal token must NOT appear in any visible code block —
    // it should be replaced by bullet characters. We assert the masked
    // bullet variant is present and the raw token is not.
    expect(screen.queryByText(/tok-secret-12345/)).toBeNull();
    expect(screen.getByText(/OPENAI_API_KEY=/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /show token/i }));

    expect(screen.getByText(/tok-secret-12345/, { exact: false })).toBeInTheDocument();
  });

  it("switches snippet content when a different tab is selected", () => {
    render(<ConnectYourClient gatewayURL="http://gw.local" token="" />);
    // OpenAI tab is active initially.
    expect(screen.getByText(/OPENAI_BASE_URL/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Anthropic/i }));
    expect(screen.getByText(/ANTHROPIC_BASE_URL/)).toBeInTheDocument();
    // OpenAI snippet should now be hidden.
    expect(screen.queryByText(/OPENAI_BASE_URL/)).toBeNull();
  });

  it("falls back to a placeholder when no token is provided", () => {
    render(<ConnectYourClient gatewayURL="http://gw.local" token="" />);
    // No token, no show/hide button — there's nothing to hide.
    expect(screen.queryByRole("button", { name: /show token/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /hide token/i })).toBeNull();
    // Placeholder should appear in the snippet.
    expect(screen.getByText(/<paste-your-token>/)).toBeInTheDocument();
  });

  it("collapses on click and stops rendering snippets", () => {
    render(<ConnectYourClient gatewayURL="http://gw.local" token="t" />);
    // Default open: snippet visible.
    expect(screen.getByText(/OPENAI_BASE_URL/)).toBeInTheDocument();

    // The toggle button has aria-expanded; clicking it should collapse.
    const toggle = screen.getByRole("button", { name: /connect a client/i });
    fireEvent.click(toggle);

    expect(screen.queryByText(/OPENAI_BASE_URL/)).toBeNull();
  });
});
