import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TracePanel } from "./TracePanel";

describe("TracePanel", () => {
  it("renders a request timeline with event attributes", () => {
    render(
      <TracePanel
        error=""
        loading={false}
        requestId="req-123"
        spans={[
          {
            trace_id: "req-123",
            span_id: "span-1",
            name: "gateway.provider",
            kind: "client",
            start_time: "2026-04-21T00:00:00Z",
            end_time: "2026-04-21T00:00:01Z",
            status_code: "ok",
            attributes: { "gen_ai.provider.name": "openai" },
            events: [
              {
                name: "request.received",
                timestamp: "2026-04-21T00:00:00Z",
                attributes: { model: "gpt-4o-mini", message_count: 1 },
              },
              {
                name: "response.returned",
                timestamp: "2026-04-21T00:00:01Z",
                attributes: { provider: "openai" },
              },
            ],
          },
        ]}
        traceStartedAt="2026-04-21T00:00:00Z"
      />,
    );

    expect(screen.getByText("Request timeline")).toBeInTheDocument();
    expect(screen.getByText("OpenTelemetry-style spans")).toBeInTheDocument();
    expect(screen.getByText("Span Events Timeline")).toBeInTheDocument();
    expect(screen.getByText("gateway.provider")).toBeInTheDocument();
    expect(screen.getByText("request.received")).toBeInTheDocument();
    expect(screen.getByText("response.returned")).toBeInTheDocument();
    expect(screen.getByText("gpt-4o-mini")).toBeInTheDocument();
    expect(screen.getAllByText("openai")[0]).toBeInTheDocument();
  });
});
