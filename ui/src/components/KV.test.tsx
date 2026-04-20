import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KV } from "./KV";

describe("KV", () => {
  it("renders the provided value", () => {
    render(
      <dl>
        <KV label="Provider" value="openai" />
      </dl>,
    );

    expect(screen.getByText("Provider")).toBeInTheDocument();
    expect(screen.getByText("openai")).toBeInTheDocument();
  });

  it("falls back to n/a for empty values", () => {
    render(
      <dl>
        <KV label="Provider" value="" />
      </dl>,
    );

    expect(screen.getByText("n/a")).toBeInTheDocument();
  });
});
