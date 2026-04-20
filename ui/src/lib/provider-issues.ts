import type { ProviderRecord } from "../types/runtime";

export type LocalProviderIssue = {
  provider: string;
  model: string;
  message: string;
  command?: string;
};

export function buildLocalProviderIssue(provider: ProviderRecord): LocalProviderIssue | null {
  if (provider.kind !== "local" || !provider.default_model) {
    return null;
  }
  if (provider.models?.includes(provider.default_model)) {
    return null;
  }

  const issue: LocalProviderIssue = {
    provider: provider.name,
    model: provider.default_model,
    message:
      "This usually means the model is not installed yet, the local runtime is not fully up, or the provider's model discovery endpoint is returning a different set than your env config expects.",
  };
  if (provider.name === "ollama") {
    issue.command = `ollama pull ${provider.default_model}`;
  }
  return issue;
}
