import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { PlaygroundPanel } from "./PlaygroundPanel";

describe("PlaygroundPanel", () => {
  it("shows semantic runtime metadata when available", () => {
    render(
      <PlaygroundPanel
        allowedModels={[]}
        allowedProviders={[]}
        chatError=""
        chatLoading={false}
        chatResult={{
          id: "chatcmpl-1",
          model: "llama3.1:8b",
          choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "Hello" } }],
          usage: { prompt_tokens: 10, completion_tokens: 4, total_tokens: 14 },
        }}
        cloudModels={[]}
        cloudProviders={[]}
        inputClassName="input"
        localModels={[]}
        localProviders={[]}
        message="hello"
        model="llama3.1:8b"
        providerFilter="ollama"
        providerScopedModels={[]}
        runtimeHeaders={{
          requestId: "req-1",
          traceId: "trace-1",
          spanId: "span-1",
          provider: "ollama",
          providerKind: "local",
          routeReason: "explicit_model",
          requestedModel: "llama3.1:8b",
          resolvedModel: "llama3.1:8b",
          cache: "true",
          cacheType: "semantic",
          semanticStrategy: "postgres_pgvector",
          semanticIndex: "hnsw",
          semanticSimilarity: "0.981234",
          attempts: "2",
          retries: "1",
          fallbackFrom: "openai",
          costUsd: "0.000001",
        }}
        tenantLocked={false}
        tenant="team-a"
        onMessageChange={vi.fn()}
        onModelChange={vi.fn()}
        onProviderFilterChange={vi.fn()}
        onSubmit={vi.fn()}
        onTenantChange={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Runtime" }));

    expect(screen.getByText("Semantic Retrieval")).toBeInTheDocument();
    expect(screen.getByText("postgres_pgvector")).toBeInTheDocument();
    expect(screen.getByText("hnsw")).toBeInTheDocument();
    expect(screen.getByText("0.981234")).toBeInTheDocument();
    expect(screen.getByText("semantic")).toBeInTheDocument();
  });

  it("shows cache type in usage view", () => {
    render(
      <PlaygroundPanel
        allowedModels={[]}
        allowedProviders={[]}
        chatError=""
        chatLoading={false}
        chatResult={{
          id: "chatcmpl-1",
          model: "gpt-4o-mini",
          choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "Hello" } }],
          usage: { prompt_tokens: 10, completion_tokens: 4, total_tokens: 14 },
        }}
        cloudModels={[]}
        cloudProviders={[]}
        inputClassName="input"
        localModels={[]}
        localProviders={[]}
        message="hello"
        model="gpt-4o-mini"
        providerFilter="auto"
        providerScopedModels={[]}
        runtimeHeaders={{
          requestId: "req-1",
          traceId: "trace-1",
          spanId: "span-1",
          provider: "openai",
          providerKind: "cloud",
          routeReason: "explicit_model",
          requestedModel: "gpt-4o-mini",
          resolvedModel: "gpt-4o-mini",
          cache: "true",
          cacheType: "semantic",
          semanticStrategy: "",
          semanticIndex: "",
          semanticSimilarity: "",
          attempts: "1",
          retries: "0",
          fallbackFrom: "",
          costUsd: "0.000123",
        }}
        tenantLocked={false}
        tenant=""
        onMessageChange={vi.fn()}
        onModelChange={vi.fn()}
        onProviderFilterChange={vi.fn()}
        onSubmit={vi.fn()}
        onTenantChange={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Usage & Cost" }));

    expect(screen.getByText("Cache Type")).toBeInTheDocument();
    expect(screen.getAllByText("semantic")[0]).toBeInTheDocument();
  });

  it("shows a semantic cache inspector in the response view", () => {
    render(
      <PlaygroundPanel
        allowedModels={[]}
        allowedProviders={[]}
        chatError=""
        chatLoading={false}
        chatResult={{
          id: "chatcmpl-1",
          model: "llama3.1:8b",
          choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "Hello" } }],
          usage: { prompt_tokens: 10, completion_tokens: 4, total_tokens: 14 },
        }}
        cloudModels={[]}
        cloudProviders={[]}
        inputClassName="input"
        localModels={[]}
        localProviders={[]}
        message="hello"
        model="llama3.1:8b"
        providerFilter="ollama"
        providerScopedModels={[]}
        runtimeHeaders={{
          requestId: "req-1",
          traceId: "trace-1",
          spanId: "span-1",
          provider: "ollama",
          providerKind: "local",
          routeReason: "explicit_model",
          requestedModel: "llama3.1:8b",
          resolvedModel: "llama3.1:8b",
          cache: "true",
          cacheType: "semantic",
          semanticStrategy: "postgres_pgvector",
          semanticIndex: "hnsw",
          semanticSimilarity: "0.981234",
          attempts: "2",
          retries: "1",
          fallbackFrom: "openai",
          costUsd: "0.000001",
        }}
        tenantLocked={false}
        tenant="team-a"
        onMessageChange={vi.fn()}
        onModelChange={vi.fn()}
        onProviderFilterChange={vi.fn()}
        onSubmit={vi.fn()}
        onTenantChange={vi.fn()}
      />,
    );

    expect(screen.getByText("Semantic Cache")).toBeInTheDocument();
    expect(screen.getByText("Retrieved via", { exact: false })).toBeInTheDocument();
    expect(screen.getAllByText("postgres_pgvector")[0]).toBeInTheDocument();
    expect(screen.getAllByText("hnsw")[0]).toBeInTheDocument();
  });

  it("shows retry and fallback metadata in runtime view", () => {
    render(
      <PlaygroundPanel
        allowedModels={[]}
        allowedProviders={[]}
        chatError=""
        chatLoading={false}
        chatResult={{
          id: "chatcmpl-1",
          model: "gpt-4o-mini",
          choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "Hello" } }],
          usage: { prompt_tokens: 10, completion_tokens: 4, total_tokens: 14 },
        }}
        cloudModels={[]}
        cloudProviders={[]}
        inputClassName="input"
        localModels={[]}
        localProviders={[]}
        message="hello"
        model="gpt-4o-mini"
        providerFilter="auto"
        providerScopedModels={[]}
        runtimeHeaders={{
          requestId: "req-2",
          traceId: "trace-2",
          spanId: "span-2",
          provider: "openai",
          providerKind: "cloud",
          routeReason: "default_model_local_first_failover",
          requestedModel: "llama3.1:8b",
          resolvedModel: "gpt-4o-mini",
          cache: "false",
          cacheType: "false",
          semanticStrategy: "",
          semanticIndex: "",
          semanticSimilarity: "",
          attempts: "2",
          retries: "0",
          fallbackFrom: "ollama",
          costUsd: "0.000123",
        }}
        tenantLocked={false}
        tenant=""
        onMessageChange={vi.fn()}
        onModelChange={vi.fn()}
        onProviderFilterChange={vi.fn()}
        onSubmit={vi.fn()}
        onTenantChange={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Runtime" }));

    expect(screen.getByText("Attempts")).toBeInTheDocument();
    expect(screen.getByText("Retries")).toBeInTheDocument();
    expect(screen.getByText("Fallback From")).toBeInTheDocument();
    expect(screen.getByText("ollama")).toBeInTheDocument();
  });
});
