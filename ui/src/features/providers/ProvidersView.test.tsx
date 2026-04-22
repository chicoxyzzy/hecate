import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProvidersView } from "./ProvidersView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

describe("ProvidersView", () => {
  it("keeps preset-backed provider setup focused on core fields first", () => {
    render(
      <ProvidersView
        actions={createRuntimeConsoleActions()}
        state={createRuntimeConsoleFixture({
          session: {
            kind: "admin",
            label: "Admin",
            capabilities: [],
            isAdmin: true,
            isAuthenticated: true,
            role: "admin",
            name: "Admin",
            tenant: "",
            source: "token",
            keyID: "",
            allowedProviders: [],
            allowedModels: [],
          },
          providerFormPresetID: "openai",
          providerFormID: "openai",
          providerFormName: "openai",
          providerFormKind: "cloud",
          providerFormProtocol: "openai",
          providerFormBaseURL: "https://api.openai.com",
          providerPresets: [
            {
              id: "openai",
              name: "OpenAI",
              kind: "cloud",
              protocol: "openai",
              base_url: "https://api.openai.com",
              default_model: "gpt-4o-mini",
              example_models: ["gpt-4o-mini"],
              description: "OpenAI preset",
            },
          ],
        })}
      />,
    );

    expect(screen.getByText("Preset-backed provider")).toBeInTheDocument();
    expect(screen.getByLabelText("Default model override")).toBeInTheDocument();
    expect(screen.queryByLabelText("Models (comma separated)")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show advanced" })).toBeInTheDocument();
  });

  it("reveals advanced provider overrides on demand", () => {
    render(
      <ProvidersView
        actions={createRuntimeConsoleActions()}
        state={createRuntimeConsoleFixture({
          session: {
            kind: "admin",
            label: "Admin",
            capabilities: [],
            isAdmin: true,
            isAuthenticated: true,
            role: "admin",
            name: "Admin",
            tenant: "",
            source: "token",
            keyID: "",
            allowedProviders: [],
            allowedModels: [],
          },
          providerFormPresetID: "ollama",
          providerFormID: "ollama",
          providerFormName: "ollama",
          providerFormKind: "local",
          providerFormProtocol: "openai",
          providerFormBaseURL: "http://127.0.0.1:11434/v1",
          providerFormAllowAnyModel: "false",
          providerPresets: [
            {
              id: "ollama",
              name: "Ollama",
              kind: "local",
              protocol: "openai",
              base_url: "http://127.0.0.1:11434/v1",
              default_model: "llama3.1:8b",
              example_models: ["llama3.1:8b"],
              description: "Local preset",
            },
          ],
        })}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Show advanced" }));

    expect(screen.getByLabelText("Base URL override")).toBeInTheDocument();
    expect(screen.getByLabelText("Models (comma separated)")).toBeInTheDocument();
    expect(screen.getByLabelText("Allow any model")).toBeInTheDocument();
  });

  it("shows inherited defaults and explicit overrides for managed providers", () => {
    render(
      <ProvidersView
        actions={createRuntimeConsoleActions()}
        state={createRuntimeConsoleFixture({
          session: {
            kind: "admin",
            label: "Admin",
            capabilities: [],
            isAdmin: true,
            isAuthenticated: true,
            role: "admin",
            name: "Admin",
            tenant: "",
            source: "token",
            keyID: "",
            allowedProviders: [],
            allowedModels: [],
          },
          controlPlane: {
            backend: "file",
            tenants: [],
            api_keys: [],
            events: [],
            providers: [
              {
                id: "groq",
                name: "groq",
                preset_id: "groq",
                kind: "cloud",
                protocol: "openai",
                base_url: "https://api.groq.com/openai/v1",
                default_model: "llama-3.3-70b-versatile",
                allow_any_model: true,
                inherited_fields: ["kind", "protocol", "base_url"],
                explicit_fields: ["default_model"],
                enabled: true,
                credential_configured: true,
              },
            ],
          },
        })}
      />,
    );

    expect(screen.getByText("preset groq")).toBeInTheDocument();
    expect(screen.getByText(/Inherits: kind, protocol, base_url/i)).toBeInTheDocument();
    expect(screen.getByText(/Overrides: default_model/i)).toBeInTheDocument();
  });
});
