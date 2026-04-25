import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Badge, CopyBtn, Dot, Icon, Icons, InlineError, Toggle } from "./ui";

describe("Toggle", () => {
  it("renders with role=switch and aria-checked", () => {
    render(<Toggle on onChange={() => {}} ariaLabel="enable feature" />);
    const sw = screen.getByRole("switch", { name: "enable feature" });
    expect(sw.getAttribute("aria-checked")).toBe("true");
  });

  it("invokes onChange with negation on click", () => {
    const onChange = vi.fn();
    render(<Toggle on={false} onChange={onChange} ariaLabel="x" />);
    fireEvent.click(screen.getByRole("switch"));
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it("supports being clicked multiple times", () => {
    const onChange = vi.fn();
    const { rerender } = render(<Toggle on={false} onChange={onChange} ariaLabel="x" />);
    fireEvent.click(screen.getByRole("switch"));
    expect(onChange).toHaveBeenLastCalledWith(true);
    rerender(<Toggle on={true} onChange={onChange} ariaLabel="x" />);
    fireEvent.click(screen.getByRole("switch"));
    expect(onChange).toHaveBeenLastCalledWith(false);
  });

  it("falls back to label when ariaLabel not given", () => {
    render(<Toggle on onChange={() => {}} label="Enabled" />);
    expect(screen.getByRole("switch", { name: "Enabled" })).toBeTruthy();
  });
});

describe("Badge", () => {
  it("renders the configured label per status", () => {
    const { rerender } = render(<Badge status="enabled" />);
    expect(screen.getByText("enabled")).toBeTruthy();
    rerender(<Badge status="error" />);
    expect(screen.getByText("error")).toBeTruthy();
    rerender(<Badge status="healthy" />);
    expect(screen.getByText("healthy")).toBeTruthy();
  });

  it("uses the provided label override", () => {
    render(<Badge status="enabled" label="active" />);
    expect(screen.getByText("active")).toBeTruthy();
  });
});

describe("InlineError", () => {
  it("renders the message inside an error block", () => {
    render(<InlineError message="something broke" />);
    expect(screen.getByText("something broke")).toBeTruthy();
  });

  it("renders nothing when message is empty", () => {
    const { container } = render(<InlineError message="" />);
    expect(container.firstChild).toBeNull();
  });
});

describe("Dot", () => {
  it("renders a circle with the requested colour", () => {
    const { container } = render(<Dot color="green" />);
    expect(container.firstChild).toBeTruthy();
  });
});

describe("Icon", () => {
  it("renders an SVG with a single path", () => {
    const { container } = render(<Icon d={Icons.chat} />);
    expect(container.querySelector("svg")).toBeTruthy();
    expect(container.querySelectorAll("path").length).toBeGreaterThan(0);
  });

  it("renders multiple paths when given an array", () => {
    const { container } = render(<Icon d={Icons.providers} />);
    expect(container.querySelectorAll("path").length).toBeGreaterThan(1);
  });
});

describe("CopyBtn", () => {
  it("invokes navigator.clipboard.writeText on click", () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    render(<CopyBtn text="hello" />);
    fireEvent.click(screen.getByRole("button"));
    expect(writeText).toHaveBeenCalledWith("hello");
  });
});
