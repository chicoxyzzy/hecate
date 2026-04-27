import { useEffect, useMemo, useState, type SyntheticEvent } from "react";

import { buildLocalProviderIssue } from "../lib/provider-issues";
import type { LocalProviderIssue } from "../lib/provider-issues";
import { filterModelsByKind, filterModelsByProvider, parseCSV, usdToMicros } from "../lib/runtime-utils";
import {
  type ChatMessage,
  chatCompletionsStream,
  createChatSession as createChatSessionRequest,
  deleteChatSession as deleteChatSessionRequest,
  updateChatSession as updateChatSessionRequest,
  deleteAPIKey as deleteAPIKeyRequest,
  deleteTenant as deleteTenantRequest,
  getAccountSummary,
  getBudget,
  getChatSession,
  getChatSessions,
  getAdminConfig,
  getHealth,
  getModels,
  getProviderPresets,
  getProviders,
  getRequestLedger,
  getRetentionRuns,
  getSession,
  rotateAPIKey as rotateAPIKeyRequest,
  setProviderAPIKey as setProviderAPIKeyRequest,
  upsertPricebookEntry as upsertPricebookEntryRequest,
  deletePricebookEntry as deletePricebookEntryRequest,
  previewPricebookImport as previewPricebookImportRequest,
  applyPricebookImport as applyPricebookImportRequest,
  runRetention as runRetentionRequest,
  resetBudget as resetBudgetRequest,
  setAPIKeyEnabled as setAPIKeyEnabledRequest,
  setBudgetLimit as setBudgetLimitRequest,
  setProviderEnabled as setProviderEnabledRequest,
  setTenantEnabled as setTenantEnabledRequest,
  topUpBudget as topUpBudgetRequest,
  upsertAPIKey as upsertAPIKeyRequest,
  upsertTenant as upsertTenantRequest,
} from "../lib/api";
import type {
  BudgetStatusResponse,
  AccountSummaryResponse,
  ChatResponse,
  ChatSessionRecord,
  ChatSessionsResponse,
  ConfiguredStateResponse,
  HealthResponse,
  ModelFilter,
  ModelResponse,
  PricebookEntryUpsertPayload,
  PricebookImportDiff,
  ProviderPresetRecord,
  ProviderFilter,
  ProviderStatusResponse,
  RequestLedgerResponse,
  RuntimeHeaders,
  SessionResponse,
  RetentionRunData,
} from "../types/runtime";

const defaultPrompt = "Say hello in one short sentence.";
type SessionKind = "anonymous" | "tenant" | "admin" | "invalid";
type SessionState = {
  kind: SessionKind;
  label: string;
  capabilities: string[];
  isAdmin: boolean;
  isAuthenticated: boolean;
  role: string;
  name: string;
  tenant: string;
  source: string;
  keyID: string;
  allowedProviders: string[];
  allowedModels: string[];
};
type NoticeState = {
  kind: "success" | "error";
  message: string;
};

const invalidBearerTokenMessage = "missing or invalid bearer token";

export function useRuntimeConsole() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [models, setModels] = useState<ModelResponse["data"]>([]);
  const [providers, setProviders] = useState<ProviderStatusResponse["data"]>([]);
  const [providerPresets, setProviderPresets] = useState<ProviderPresetRecord[]>([]);
  const [budget, setBudget] = useState<BudgetStatusResponse["data"] | null>(null);
  const [accountSummary, setAccountSummary] = useState<AccountSummaryResponse["data"] | null>(null);
  const [requestLedger, setRequestLedger] = useState<RequestLedgerResponse["data"]>([]);
  const [adminConfig, setAdminConfig] = useState<ConfiguredStateResponse["data"] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [model, setModel] = useState("");
  const [tenant, setTenant] = useState("");
  const [message, setMessage] = useState(defaultPrompt);
  const [systemPrompt, setSystemPrompt] = useState("");
  const [chatLoading, setChatLoading] = useState(false);
  const [streamingContent, setStreamingContent] = useState<string | null>(null);
  const [chatResult, setChatResult] = useState<ChatResponse | null>(null);
  // pendingToolCalls: model responded with tool_calls; waiting for user to fill results.
  const [pendingToolCalls, setPendingToolCalls] = useState<Array<{ id: string; name: string; arguments: string; result: string }>>([]);
  // Thread of messages that preceded the pending tool calls (history + user message + assistant tool_calls message).
  const [pendingThread, setPendingThread] = useState<import("../lib/api").ChatMessage[] | null>(null);
  const [chatSessions, setChatSessions] = useState<ChatSessionsResponse["data"]>([]);
  const [chatSessionsHasMore, setChatSessionsHasMore] = useState(false);
  const [chatSessionsLoadingMore, setChatSessionsLoadingMore] = useState(false);
  const [activeChatSessionID, setActiveChatSessionID] = useState("");
  const [activeChatSession, setActiveChatSession] = useState<ChatSessionRecord | null>(null);
  const [runtimeHeaders, setRuntimeHeaders] = useState<RuntimeHeaders | null>(null);
  const [chatError, setChatError] = useState("");
  const [modelFilter, setModelFilter] = useState<ModelFilter>("all");
  const [providerFilter, setProviderFilter] = useState<ProviderFilter>("auto");
  const [copiedCommand, setCopiedCommand] = useState("");

  const [budgetAmountUsd, setBudgetAmountUsd] = useState("1.00");
  const [budgetLimitUsd, setBudgetLimitUsd] = useState("5.00");
  const [budgetActionError, setBudgetActionError] = useState("");

  // Lazy-init from localStorage so the very first render already knows
  // whether we have a token. Otherwise the gate flashes the workspace
  // shell with stale data on every refresh before TokenGate can mount.
  const [authToken, setAuthToken] = useState<string>(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem("hecate.authToken") ?? "";
  });
  const [sessionInfo, setSessionInfo] = useState<SessionResponse["data"] | null>(null);
  const [adminConfigError, setAdminConfigError] = useState("");
  const [notice, setNotice] = useState<NoticeState | null>(null);

  const [tenantFormName, setTenantFormName] = useState("");
  const [tenantFormID, setTenantFormID] = useState("");
  const [tenantFormProviders, setTenantFormProviders] = useState("");
  const [tenantFormModels, setTenantFormModels] = useState("");

  const [apiKeyFormName, setAPIKeyFormName] = useState("");
  const [apiKeyFormID, setAPIKeyFormID] = useState("");
  const [apiKeyFormSecret, setAPIKeyFormSecret] = useState("");
  const [apiKeyFormTenant, setAPIKeyFormTenant] = useState("");
  const [apiKeyFormRole, setAPIKeyFormRole] = useState("tenant");
  const [apiKeyFormProviders, setAPIKeyFormProviders] = useState("");
  const [apiKeyFormModels, setAPIKeyFormModels] = useState("");
  const [rotateAPIKeyID, setRotateAPIKeyID] = useState("");
  const [rotateAPIKeySecret, setRotateAPIKeySecret] = useState("");
  const [retentionSubsystems, setRetentionSubsystems] = useState("");
  const [retentionLoading, setRetentionLoading] = useState(false);
  const [retentionError, setRetentionError] = useState("");
  const [retentionLastRun, setRetentionLastRun] = useState<RetentionRunData | null>(null);
  const [retentionRuns, setRetentionRuns] = useState<RetentionRunData[]>([]);

  const healthyProviders = providers.filter((provider) => provider.healthy).length;
  const localProviders = providers.filter((provider) => provider.kind === "local");
  const cloudProviders = providers.filter((provider) => provider.kind === "cloud");
  const localModels = models.filter((entry) => entry.metadata?.provider_kind === "local");
  const cloudModels = models.filter((entry) => entry.metadata?.provider_kind === "cloud");
  const healthyLocalProviders = localProviders.filter((provider) => provider.healthy).length;
  const healthyCloudProviders = cloudProviders.filter((provider) => provider.healthy).length;

  const visibleModels = useMemo(() => filterModelsByKind(models, modelFilter), [modelFilter, models]);
  const providerScopedModels = useMemo(
    () => filterModelsByProvider(visibleModels, providerFilter),
    [providerFilter, visibleModels],
  );
  const localProviderIssues = useMemo(
    () =>
      localProviders
        .map((provider) => buildLocalProviderIssue(provider))
        .filter((issue): issue is LocalProviderIssue => issue !== null),
    [localProviders],
  );
  const session = useMemo(() => {
    return deriveSessionState(sessionInfo);
  }, [sessionInfo]);

  useEffect(() => {
    // authToken is hydrated synchronously above via the useState lazy init.
    const storedChatSessionID = window.localStorage.getItem("hecate.chatSessionID");
    if (storedChatSessionID) {
      setActiveChatSessionID(storedChatSessionID);
    }
    const storedModel = window.localStorage.getItem("hecate.model");
    if (storedModel) {
      setModel(storedModel);
    }
    const storedProvider = window.localStorage.getItem("hecate.providerFilter");
    if (storedProvider) {
      setProviderFilter(storedProvider as ProviderFilter);
    }
    const storedSystemPrompt = window.localStorage.getItem("hecate.systemPrompt");
    if (storedSystemPrompt) {
      setSystemPrompt(storedSystemPrompt);
    }
  }, []);

  useEffect(() => {
    window.localStorage.setItem("hecate.systemPrompt", systemPrompt);
  }, [systemPrompt]);

  useEffect(() => {
    // TokenGate renders when authToken is empty; firing the dashboard
    // anyway would 401-spam the eight admin/auth-required endpoints in
    // the console for no benefit. Flip loading to false since there's
    // nothing to load — anything observing `state.loading` (TokenGate's
    // gate, the AuthLoadingShell splash) needs an authoritative answer.
    if (!authToken) {
      setLoading(false);
      return;
    }
    void loadDashboard();
  }, [authToken]);

  useEffect(() => {
    window.localStorage.setItem("hecate.authToken", authToken);
  }, [authToken]);

  useEffect(() => {
    if (model) {
      window.localStorage.setItem("hecate.model", model);
    }
  }, [model]);

  useEffect(() => {
    window.localStorage.setItem("hecate.providerFilter", providerFilter);
  }, [providerFilter]);

  useEffect(() => {
    if (activeChatSessionID) {
      window.localStorage.setItem("hecate.chatSessionID", activeChatSessionID);
      return;
    }
    window.localStorage.removeItem("hecate.chatSessionID");
  }, [activeChatSessionID]);

  useEffect(() => {
    if (!notice) {
      return;
    }
    const timeout = window.setTimeout(() => {
      setNotice((current) => (current === notice ? null : current));
    }, 3000);
    return () => window.clearTimeout(timeout);
  }, [notice]);

  useEffect(() => {
    if (providerFilter === "auto") {
      return;
    }
    const stillValid = isModelValidForProvider(model, providerFilter, models, providers, providerPresets);
    if (stillValid) {
      return;
    }
    const nextModel = defaultModelForProvider(providerFilter, models, providers, providerPresets);
    setModel(nextModel);
  }, [model, models, providerFilter, providers, providerPresets]);

  useEffect(() => {
    if (session.kind !== "tenant" || !session.tenant) {
      return;
    }
    setTenant((current) => (current === session.tenant ? current : session.tenant));
  }, [session.kind, session.tenant]);

  useEffect(() => {
    if (providerFilter === "auto" || model !== "" || models.length === 0) {
      return;
    }
    const scopedModels = models.filter((m) => m.metadata?.provider === providerFilter);
    if (scopedModels.length === 0) return;
    setModel(defaultModelForProvider(providerFilter, models, providers, providerPresets));
  }, [model, models, providers, providerFilter, providerPresets]);

  // When models load, validate the selected model. If it's not in the list (e.g. stale localStorage),
  // fall back to the gateway default. If no model is set at all, pick the default.
  useEffect(() => {
    if (models.length === 0) return;
    if (model !== "" && models.some((m) => m.id === model)) return;
    const defaultM = models.find((m) => m.metadata?.default)?.id ?? models[0]?.id ?? "";
    if (defaultM) setModel(defaultM);
  }, [model, models]);

  useEffect(() => {
    if (providerFilter !== "auto" && session.allowedProviders.length > 0 && !session.allowedProviders.includes(providerFilter)) {
      setProviderFilter("auto");
      return;
    }

    if (session.allowedModels.length > 0 && model !== "" && !session.allowedModels.includes(model)) {
      const nextAllowedModel =
        models.find((entry) => session.allowedModels.includes(entry.id) && (providerFilter === "auto" || entry.metadata?.provider === providerFilter))?.id ??
        models.find((entry) => session.allowedModels.includes(entry.id))?.id ??
        "";
      setModel(nextAllowedModel);
    }
  }, [model, models, providerFilter, session.allowedModels, session.allowedProviders]);

  function clearPendingToolState() {
    setPendingToolCalls([]);
    setPendingThread(null);
  }

  function resetChatWorkspaceState() {
    setChatResult(null);
    setStreamingContent(null);
    setRuntimeHeaders(null);
    clearPendingToolState();
    setChatError("");
    setSystemPrompt("");
  }

  function activateChatSession(sessionRecord: ChatSessionRecord) {
    setActiveChatSessionID(sessionRecord.id);
    setActiveChatSession(sessionRecord);
  }

  function upsertChatSessionSummary(sessionRecord: ChatSessionRecord) {
    setChatSessions((current) => [renderChatSessionSummary(sessionRecord), ...current.filter((entry) => entry.id !== sessionRecord.id)]);
  }

  async function createChatSessionRecord(title: string): Promise<ChatSessionRecord> {
    const payload = await createChatSessionRequest({ title }, authToken);
    activateChatSession(payload.data);
    upsertChatSessionSummary(payload.data);
    return payload.data;
  }

  async function refreshChatSessionState(sessionID: string) {
    if (!sessionID) {
      return;
    }
    try {
      const [sessionsResult, sessionResult] = await Promise.all([
        getChatSessions(authToken, 20),
        getChatSession(sessionID, authToken),
      ]);
      setChatSessions(sessionsResult.data ?? []);
      setActiveChatSession(sessionResult.data);
    } catch {
      // Keep the primary request flow resilient.
    }
  }

  async function refreshAdminRuntimeState() {
    if (!session.isAdmin) {
      return;
    }
    try {
      const [accountSummaryResult, requestLedgerResult] = await Promise.all([
        getAccountSummary("", authToken),
        getRequestLedger(authToken, 20),
      ]);
      setAccountSummary(accountSummaryResult.data);
      setRequestLedger(requestLedgerResult.data ?? []);
    } catch {
      // Keep chat responsive even if admin-only refresh paths fail.
    }
  }

  function buildChatPayload(messages: ChatMessage[], sessionID?: string) {
    return {
      model,
      provider: providerFilter === "auto" ? "" : providerFilter,
      session_id: sessionID,
      user: tenant,
      messages,
    };
  }

  function resetTenantForm() {
    setTenantFormID("");
    setTenantFormName("");
    setTenantFormProviders("");
    setTenantFormModels("");
  }

  function resetAPIKeyForm() {
    setAPIKeyFormID("");
    setAPIKeyFormName("");
    setAPIKeyFormSecret("");
    setAPIKeyFormTenant("");
    setAPIKeyFormProviders("");
    setAPIKeyFormModels("");
  }

  function resetRotateAPIKeyForm() {
    setRotateAPIKeyID("");
    setRotateAPIKeySecret("");
  }

  async function loadDashboard() {
    setLoading(true);
    setError("");
    setAdminConfigError("");

    try {
      const snapshot = await resolveDashboardSnapshot({
        authToken,
        activeChatSessionID,
        previous: {
          providers,
          budget,
          accountSummary,
          chatSessions,
          activeChatSession,
          requestLedger,
          adminConfig,
          retentionRuns,
          retentionLastRun,
        },
      });

      setHealth(snapshot.health);
      setSessionInfo(snapshot.sessionInfo);
      setModels(snapshot.models);
      setProviders(snapshot.providers);
      setProviderPresets(snapshot.providerPresets);
      setBudget(snapshot.budget);
      setAccountSummary(snapshot.accountSummary);
      setChatSessions(snapshot.chatSessions);
      setChatSessionsHasMore(snapshot.chatSessionsHasMore);
      setActiveChatSessionID(snapshot.activeChatSessionID);
      setActiveChatSession(snapshot.activeChatSession);
      setRequestLedger(snapshot.requestLedger);
      setAdminConfig(snapshot.adminConfig);
      setRetentionRuns(snapshot.retentionRuns);
      setRetentionLastRun(snapshot.retentionLastRun);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "unknown load error");
    } finally {
      setLoading(false);
    }
  }

  function selectProviderRoute(nextProvider: ProviderFilter) {
    setProviderFilter(nextProvider);
    setModel(defaultModelForProvider(nextProvider, models, providers, providerPresets));
  }

  async function submitChat(event: SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    setChatLoading(true);
    setChatError("");
    setRuntimeHeaders(null);

    try {
      let sessionID = activeChatSessionID;
      if (!sessionID) {
        const createdSession = await createChatSessionRecord(deriveChatSessionTitle(message));
        sessionID = createdSession.id;
      }

      const messages = buildMessagesForSubmission(activeChatSession, message, systemPrompt);
      clearPendingToolState();

      // Show the user message immediately, before streaming starts.
      const optimisticMessage = message;
      setMessage("");
      setActiveChatSession((prev) =>
        prev
          ? {
              ...prev,
              turns: [
                ...(prev.turns ?? []),
                {
                  id: `pending-${Date.now()}`,
                  request_id: "",
                  user_message: { role: "user", content: optimisticMessage },
                  assistant_message: { role: "assistant", content: null },
                  provider: "",
                  model: "",
                  cost_micros_usd: 0,
                  cost_usd: "0",
                  prompt_tokens: 0,
                  completion_tokens: 0,
                  total_tokens: 0,
                  created_at: new Date().toISOString(),
                },
              ],
            }
          : prev,
      );

      const chatExecution = await executeChatRequest(buildChatPayload(messages, sessionID), messages);
      if (chatExecution.kind === "tool_calls") {
        return;
      }
      const { headers } = chatExecution;

      // Patch the optimistic turn with the real assistant content so it's visible
      // immediately, regardless of whether the backend session refresh wins the race.
      const assistantContent = chatExecution.chatResult.choices[0]?.message.content ?? "";
      setActiveChatSession((prev) => {
        if (!prev?.turns?.length) return prev;
        const turns = [...prev.turns];
        const last = turns[turns.length - 1];
        if (last.id.startsWith("pending-")) {
          turns[turns.length - 1] = {
            ...last,
            assistant_message: { role: "assistant", content: assistantContent },
            model: headers.resolvedModel || model,
          };
        }
        return { ...prev, turns };
      });

      setChatResult(chatExecution.chatResult);

      try {
        const scopedBudget = await getBudget(
          `?scope=tenant_provider&tenant=${encodeURIComponent(tenant)}&provider=${encodeURIComponent(headers.provider)}`,
          authToken,
        );
        setBudget(scopedBudget.data);
      } catch {
        // Tenant-key users may not be authorized for admin budget views.
      }

      await refreshChatSessionState(sessionID);
      setStreamingContent(null);
      await refreshAdminRuntimeState();
    } catch (submitError) {
      setChatError(submitError instanceof Error ? submitError.message : "unknown request error");
    } finally {
      setChatLoading(false);
    }
  }

  function updateToolResult(index: number, result: string) {
    setPendingToolCalls((prev) => prev.map((tc, i) => (i === index ? { ...tc, result } : tc)));
  }

  async function submitToolResults() {
    if (!pendingThread || pendingToolCalls.length === 0) return;
    setChatLoading(true);
    setChatError("");

    const toolMessages: ChatMessage[] = pendingToolCalls.map((tc) => ({
      role: "tool" as const,
      content: tc.result,
      tool_call_id: tc.id,
    }));

    const messages: ChatMessage[] = [...pendingThread, ...toolMessages];

    try {
      const chatExecution = await executeChatRequest(buildChatPayload(messages, activeChatSessionID || undefined), messages);
      if (chatExecution.kind === "tool_calls") {
        return;
      }

      clearPendingToolState();
      setChatResult(chatExecution.chatResult);
      await refreshChatSessionState(activeChatSessionID);
      setStreamingContent(null);
      await refreshAdminRuntimeState();
    } catch (err) {
      setChatError(err instanceof Error ? err.message : "unknown error");
    } finally {
      setChatLoading(false);
    }
  }

  async function executeChatRequest(
    chatPayload: {
      model: string;
      provider: string;
      session_id?: string;
      user: string;
      messages: ChatMessage[];
    },
    toolCallBaseMessages: ChatMessage[],
  ): Promise<
    | { kind: "tool_calls" }
    | { kind: "completed"; headers: RuntimeHeaders; chatResult: ChatResponse }
  > {
    let fullContent = "";
    setStreamingContent("");
    const response = await chatCompletionsStream(chatPayload, authToken, (delta) => {
      fullContent += delta;
      setStreamingContent(fullContent);
    });
    setRuntimeHeaders(response.headers);

    if (response.finishReason === "tool_calls" && response.toolCalls.length > 0) {
      setStreamingContent(null);
      const assistantMsg = buildAssistantToolCallMessage(fullContent, response.toolCalls);
      setPendingThread([...toolCallBaseMessages, assistantMsg]);
      setPendingToolCalls(response.toolCalls.map((tc) => ({ ...tc, result: "" })));
      return { kind: "tool_calls" };
    }

    return {
      kind: "completed",
      headers: response.headers,
      chatResult: buildSyntheticChatResult(response.headers, model, fullContent),
    };
  }

  async function resetBudget() {
    if (!budget) {
      return;
    }
    setBudgetActionError("");
    setNotice(null);

    if (!window.confirm("Reset tracked budget usage for the current scope?")) {
      return;
    }

    try {
      await resetBudgetRequest(
        {
          scope: budget.scope,
          provider: budget.provider,
          tenant: budget.tenant,
          key: budget.scope === "custom" ? budget.key : "",
        },
        authToken,
      );
      await loadDashboard();
      setNotice({ kind: "success", message: "Budget usage reset." });
      return;
    } catch {
      setBudgetActionError("failed to reset budget usage");
      setNotice({ kind: "error", message: "Failed to reset budget usage." });
    }
  }

  async function topUpBudget() {
    if (!budget) {
      return;
    }
    setBudgetActionError("");

    const amountMicrosUSD = usdToMicros(budgetAmountUsd);
    if (!Number.isFinite(amountMicrosUSD) || amountMicrosUSD <= 0) {
      setBudgetActionError("top-up amount must be greater than zero");
      return;
    }

    try {
      await topUpBudgetRequest(
        {
          scope: budget.scope,
          provider: budget.provider,
          tenant: budget.tenant,
          key: budget.scope === "custom" ? budget.key : "",
          amount_micros_usd: amountMicrosUSD,
        },
        authToken,
      );
      await loadDashboard();
      setNotice({ kind: "success", message: "Budget topped up." });
      return;
    } catch (error) {
      setBudgetActionError(error instanceof Error ? error.message : "failed to top up budget");
      setNotice({ kind: "error", message: "Failed to top up budget." });
    }
  }

  async function setBudgetLimit() {
    if (!budget) {
      return;
    }
    setBudgetActionError("");

    const limitMicrosUSD = usdToMicros(budgetLimitUsd);
    if (!Number.isFinite(limitMicrosUSD) || limitMicrosUSD < 0) {
      setBudgetActionError("limit must be zero or greater");
      return;
    }

    try {
      await setBudgetLimitRequest(
        {
          scope: budget.scope,
          provider: budget.provider,
          tenant: budget.tenant,
          key: budget.scope === "custom" ? budget.key : "",
          balance_micros_usd: limitMicrosUSD,
        },
        authToken,
      );
      await loadDashboard();
      setNotice({ kind: "success", message: "Budget limit updated." });
      return;
    } catch (error) {
      setBudgetActionError(error instanceof Error ? error.message : "failed to set budget limit");
      setNotice({ kind: "error", message: "Failed to update budget limit." });
    }
  }

  function setNoticeMessage(kind: NoticeState["kind"], message: string) {
    if (message) setNotice({ kind, message });
  }

  function describeError(error: unknown, fallback: string): string {
    return error instanceof Error ? error.message : fallback;
  }

  function resetAdminFeedback() {
    setAdminConfigError("");
    setNotice(null);
  }

  async function runAdminMutation(options: {
    action: () => Promise<void>;
    successMessage: string;
    errorMessage: string;
    failureDetail: string;
  }) {
    resetAdminFeedback();
    try {
      await options.action();
      await loadDashboard();
      setNoticeMessage("success", options.successMessage);
    } catch (error) {
      setAdminConfigError(describeError(error, options.failureDetail));
      setNoticeMessage("error", options.errorMessage);
    }
  }

  async function upsertTenant() {
    await runAdminMutation({
      successMessage: "Tenant saved.",
      errorMessage: "Failed to save tenant.",
      failureDetail: "failed to save tenant",
      action: async () => {
        await upsertTenantRequest(
          {
            id: tenantFormID,
            name: tenantFormName,
            allowed_providers: parseCSV(tenantFormProviders),
            allowed_models: parseCSV(tenantFormModels),
            enabled: true,
          },
          authToken,
        );
        resetTenantForm();
      },
    });
  }

  async function upsertAPIKey() {
    await upsertAPIKeyRequest(
      {
        id: apiKeyFormID,
        name: apiKeyFormName,
        key: apiKeyFormSecret,
        tenant: apiKeyFormTenant,
        role: apiKeyFormRole,
        allowed_providers: parseCSV(apiKeyFormProviders),
        allowed_models: parseCSV(apiKeyFormModels),
        enabled: true,
      },
      authToken,
    );
    resetAPIKeyForm();
    void loadDashboard();
  }

  // setProviderAPIKey is the single operation for managing a provider's API key.
  // An empty `key` clears the existing credential; non-empty sets/replaces it.
  async function setProviderAPIKey(id: string, key: string) {
    await runAdminMutation({
      successMessage: key === "" ? "API key cleared." : "API key saved.",
      errorMessage: key === "" ? "Failed to clear API key." : "Failed to save API key.",
      failureDetail: key === "" ? "failed to clear provider api key" : "failed to save provider api key",
      action: async () => {
        await setProviderAPIKeyRequest(id, key, authToken);
      },
    });
  }

  async function setProviderEnabled(id: string, enabled: boolean) {
    await runAdminMutation({
      successMessage: "",
      errorMessage: "Failed to update provider state.",
      failureDetail: "failed to update provider state",
      action: async () => {
        await setProviderEnabledRequest(id, enabled, authToken);
      },
    });
  }

  async function setTenantEnabled(id: string, enabled: boolean) {
    await runAdminMutation({
      successMessage: `Tenant ${enabled ? "enabled" : "disabled"}.`,
      errorMessage: "Failed to update tenant state.",
      failureDetail: "failed to update tenant state",
      action: async () => {
        await setTenantEnabledRequest({ id, enabled }, authToken);
      },
    });
  }

  async function deleteTenant(id: string) {
    resetAdminFeedback();
    if (!window.confirm(`Delete tenant "${id}"? This cannot be undone.`)) {
      return;
    }
    await runAdminMutation({
      successMessage: "Tenant deleted.",
      errorMessage: "Failed to delete tenant.",
      failureDetail: "failed to delete tenant",
      action: async () => {
        await deleteTenantRequest({ id }, authToken);
      },
    });
  }

  async function setAPIKeyEnabled(id: string, enabled: boolean) {
    await runAdminMutation({
      successMessage: `API key ${enabled ? "enabled" : "disabled"}.`,
      errorMessage: "Failed to update API key state.",
      failureDetail: "failed to update api key state",
      action: async () => {
        await setAPIKeyEnabledRequest({ id, enabled }, authToken);
      },
    });
  }

  async function rotateAPIKey() {
    await runAdminMutation({
      successMessage: "API key rotated.",
      errorMessage: "Failed to rotate API key.",
      failureDetail: "failed to rotate api key",
      action: async () => {
        await rotateAPIKeyRequest({ id: rotateAPIKeyID, key: rotateAPIKeySecret }, authToken);
        resetRotateAPIKeyForm();
      },
    });
  }

  async function deleteAPIKey(id: string) {
    resetAdminFeedback();
    if (!window.confirm(`Delete API key "${id}"? This cannot be undone.`)) {
      return;
    }
    await runAdminMutation({
      successMessage: "API key deleted.",
      errorMessage: "Failed to delete API key.",
      failureDetail: "failed to delete api key",
      action: async () => {
        await deleteAPIKeyRequest({ id }, authToken);
      },
    });
  }

  async function upsertPricebookEntry(entry: PricebookEntryUpsertPayload) {
    await runAdminMutation({
      successMessage: "Pricebook entry saved.",
      errorMessage: "Failed to save pricebook entry.",
      failureDetail: "failed to save pricebook entry",
      action: async () => {
        await upsertPricebookEntryRequest(entry, authToken);
      },
    });
  }

  async function deletePricebookEntry(provider: string, model: string) {
    // Confirmation is the caller's concern now (PricebookTab routes
    // this through a styled ConfirmModal). The action itself just
    // performs the deletion.
    resetAdminFeedback();
    await runAdminMutation({
      successMessage: "Price cleared.",
      errorMessage: "Failed to clear price.",
      failureDetail: "failed to clear pricebook entry",
      action: async () => {
        await deletePricebookEntryRequest(provider, model, authToken);
      },
    });
  }

  // previewPricebookImport intentionally does NOT call runAdminMutation —
  // it doesn't mutate anything. It just fetches the diff and lets the
  // caller (the import modal) render it.
  async function previewPricebookImport(): Promise<PricebookImportDiff> {
    const response = await previewPricebookImportRequest(authToken);
    return response.data;
  }

  async function applyPricebookImport(keys: string[]): Promise<PricebookImportDiff> {
    const response = await applyPricebookImportRequest(keys, authToken);
    await loadDashboard();
    // Notice text varies with the partial-success outcome so the
    // operator sees the exact tally — silent "import applied" was
    // misleading when one or more rows actually failed.
    const data = response.data;
    const appliedCount = data.applied?.length ?? 0;
    const failedCount = data.failed?.length ?? 0;
    if (failedCount > 0 && appliedCount > 0) {
      setNoticeMessage("error", `Imported ${appliedCount}, ${failedCount} failed.`);
    } else if (failedCount > 0) {
      setNoticeMessage("error", `Import failed for ${failedCount} ${failedCount === 1 ? "row" : "rows"}.`);
    } else {
      setNoticeMessage("success", `Imported ${appliedCount} ${appliedCount === 1 ? "row" : "rows"}.`);
    }
    return data;
  }

  async function copyCommand(command: string) {
    try {
      await navigator.clipboard.writeText(command);
      setCopiedCommand(command);
      window.setTimeout(() => {
        setCopiedCommand((current) => (current === command ? "" : current));
      }, 1500);
    } catch {
      setCopiedCommand("");
    }
  }

  async function runRetention() {
    setRetentionError("");
    setNotice(null);
    setRetentionLoading(true);
    try {
      const payload = await runRetentionRequest(
        {
          subsystems: parseCSV(retentionSubsystems),
        },
        authToken,
      );
      setRetentionLastRun(payload.data);
      setRetentionRuns((current) => [payload.data, ...current.filter((run) => run.finished_at !== payload.data.finished_at)].slice(0, 10));
      setNotice({ kind: "success", message: "Retention run completed." });
    } catch (error) {
      setRetentionError(error instanceof Error ? error.message : "failed to run retention");
      setNotice({ kind: "error", message: "Failed to run retention." });
    } finally {
      setRetentionLoading(false);
    }
  }

  function createChatSession() {
    startNewChat();
  }

  async function selectChatSession(id: string) {
    setActiveChatSessionID(id);
    if (!id) {
      setActiveChatSession(null);
      return;
    }
    try {
      const payload = await getChatSession(id, authToken);
      setActiveChatSession(payload.data);
    } catch (error) {
      setChatError(error instanceof Error ? error.message : "failed to load chat session");
    }
  }

  function startNewChat() {
    setActiveChatSessionID("");
    setActiveChatSession(null);
    resetChatWorkspaceState();
  }

  async function deleteChatSession(id: string) {
    try {
      await deleteChatSessionRequest(id, authToken);
      setChatSessions((current) => current.filter((s) => s.id !== id));
      if (activeChatSessionID === id) {
        startNewChat();
      }
      setNoticeMessage("success", "Session deleted.");
    } catch (error) {
      setNoticeMessage("error", error instanceof Error ? error.message : "Failed to delete session.");
    }
  }

  async function renameChatSession(id: string, title: string) {
    try {
      const payload = await updateChatSessionRequest(id, title, authToken);
      setChatSessions((current) =>
        current.map((s) => (s.id === id ? { ...s, title: payload.data.title } : s)),
      );
      if (activeChatSessionID === id) {
        setActiveChatSession((current) => (current ? { ...current, title: payload.data.title } : current));
      }
    } catch (error) {
      setNoticeMessage("error", error instanceof Error ? error.message : "Failed to rename session.");
    }
  }

  async function loadMoreChatSessions() {
    if (chatSessionsLoadingMore || !chatSessionsHasMore) return;
    setChatSessionsLoadingMore(true);
    try {
      const result = await getChatSessions(authToken, 20, chatSessions.length);
      setChatSessions((current) => [...current, ...(result.data ?? [])]);
      setChatSessionsHasMore(result.has_more ?? false);
    } catch {
      // Keep sidebar responsive; silently skip failed page loads.
    } finally {
      setChatSessionsLoadingMore(false);
    }
  }

  return {
    state: {
      apiKeyFormID,
      apiKeyFormModels,
      apiKeyFormName,
      apiKeyFormProviders,
      apiKeyFormRole,
      apiKeyFormSecret,
      apiKeyFormTenant,
      authToken,
      budget,
      accountSummary,
      requestLedger,
      budgetActionError,
      budgetAmountUsd,
      budgetLimitUsd,
      chatError,
      chatLoading,
      streamingContent,
      chatResult,
      pendingToolCalls,
      chatSessions,
      cloudModels,
      cloudProviders,
      adminConfig,
      adminConfigError,
      copiedCommand,
      error,
      health,
      healthyCloudProviders,
      healthyLocalProviders,
      healthyProviders,
      loading,
      localModels,
      localProviderIssues,
      localProviders,
      message,
      systemPrompt,
      model,
      modelFilter,
      models,
      notice,
      session,
      providerFilter,
      providerScopedModels,
      providers,
      providerPresets,
      activeChatSession,
      activeChatSessionID,
      retentionError,
      retentionLastRun,
      retentionLoading,
      retentionRuns,
      retentionSubsystems,
      rotateAPIKeyID,
      rotateAPIKeySecret,
      runtimeHeaders,
      chatSessionsHasMore,
      chatSessionsLoadingMore,
      tenant,
      tenantFormID,
      tenantFormModels,
      tenantFormName,
      tenantFormProviders,
      visibleModels,
    },
    actions: {
      copyCommand,
      deleteAPIKey,
      deleteTenant,
      createChatSession,
      deleteChatSession,
      renameChatSession,
      loadDashboard,
      resetBudget,
      rotateAPIKey,
      setAPIKeyEnabled,
      setAPIKeyFormID,
      setAPIKeyFormModels,
      setAPIKeyFormName,
      setAPIKeyFormProviders,
      setAPIKeyFormRole,
      setAPIKeyFormSecret,
      setAPIKeyFormTenant,
      setAuthToken,
      setBudgetAmountUsd,
      setBudgetLimitUsd,
      setMessage,
      setSystemPrompt,
      setModel,
      setModelFilter,
      setProviderFilter: selectProviderRoute,
      setProviderEnabled,
      setRetentionSubsystems,
      setRotateAPIKeyID,
      setRotateAPIKeySecret,
      setTenantEnabled,
      setTenant,
      setTenantFormID,
      setTenantFormModels,
      setTenantFormName,
      setTenantFormProviders,
      setBudgetLimit,
      runRetention,
      selectChatSession,
      startNewChat,
      submitChat,
      loadMoreChatSessions,
      submitToolResults,
      updateToolResult,
      topUpBudget,
      upsertAPIKey,
      setProviderAPIKey,
      upsertPricebookEntry,
      deletePricebookEntry,
      previewPricebookImport,
      applyPricebookImport,
      upsertTenant,
      clearAuthToken: () => setAuthToken(""),
      dismissNotice: () => setNotice(null),
    },
  };
}

function deriveChatSessionTitle(message: string): string {
  const normalized = message.trim().replace(/\s+/g, " ");
  if (!normalized) {
    return "New chat";
  }
  if (normalized.length <= 48) {
    return normalized;
  }
  return `${normalized.slice(0, 45)}...`;
}

function buildMessagesForSubmission(activeSession: ChatSessionRecord | null, message: string, systemPrompt = ""): ChatMessage[] {
  const history: ChatMessage[] =
    activeSession?.turns?.flatMap((turn) => {
      const user: ChatMessage = { role: "user", content: turn.user_message.content ?? "" };
      const assistant: ChatMessage = turn.assistant_message.tool_calls?.length
        ? { role: "assistant", content: turn.assistant_message.content ?? null, tool_calls: turn.assistant_message.tool_calls }
        : { role: "assistant", content: turn.assistant_message.content ?? "" };
      return [user, assistant];
    }) ?? [];
  const prefix: ChatMessage[] = systemPrompt.trim() ? [{ role: "system", content: systemPrompt.trim() }] : [];
  return [...prefix, ...history, { role: "user", content: message }];
}

function buildAssistantToolCallMessage(
  content: string,
  toolCalls: Array<{ id: string; name: string; arguments: string }>,
): ChatMessage {
  return {
    role: "assistant",
    content: content || null,
    tool_calls: toolCalls.map((tc) => ({
      id: tc.id,
      type: "function",
      function: { name: tc.name, arguments: tc.arguments },
    })),
  };
}

function buildSyntheticChatResult(headers: RuntimeHeaders, selectedModel: string, content: string): ChatResponse {
  return {
    id: headers.requestId || "stream",
    model: headers.resolvedModel || selectedModel,
    choices: [{ index: 0, message: { role: "assistant", content }, finish_reason: "stop" }],
    usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
  };
}

function defaultModelForProvider(provider: ProviderFilter, models: ModelResponse["data"], providers: ProviderStatusResponse["data"], presets: ProviderPresetRecord[]): string {
  if (provider === "auto") {
    return "";
  }

	const providerRecord = providers.find((entry) => entry.name === provider);
	const scopedModels = models.filter((entry) => entry.metadata?.provider === provider);
	const preset = presets.find((entry) => entry.id === provider);
	if (providerRecord?.default_model) {
		return providerRecord.default_model;
	}

	if (providerRecord) {
		return scopedModels.find((entry) => entry.metadata?.default)?.id ?? scopedModels[0]?.id ?? providerRecord.models?.[0] ?? "";
	}

	return scopedModels.find((entry) => entry.metadata?.default)?.id ?? scopedModels[0]?.id ?? preset?.default_model ?? "";
}

function isModelValidForProvider(model: string, provider: ProviderFilter, models: ModelResponse["data"], providers: ProviderStatusResponse["data"], presets: ProviderPresetRecord[]): boolean {
  if (!model || provider === "auto") {
    return true;
  }

  if (models.some((entry) => entry.id === model && entry.metadata?.provider === provider)) {
    return true;
  }

	const providerRecord = providers.find((entry) => entry.name === provider);
	if (providerRecord?.default_model === model || providerRecord?.models?.includes(model)) {
		return true;
	}
	if (providerRecord) {
		return false;
	}

	const preset = presets.find((entry) => entry.id === provider);
	return preset?.default_model === model;
}

function renderChatSessionSummary(session: ChatSessionRecord): ChatSessionsResponse["data"][number] {
  const turns = session.turns ?? [];
  const lastTurn = turns[turns.length - 1];
  return {
    id: session.id,
    title: session.title,
    tenant: session.tenant,
    user: session.user,
    turn_count: turns.length,
    created_at: session.created_at,
    updated_at: session.updated_at,
    last_model: lastTurn?.model,
    last_provider: lastTurn?.provider,
    last_cost_usd: lastTurn?.cost_usd,
    last_request_id: lastTurn?.request_id,
  };
}

export type RuntimeConsoleViewModel = ReturnType<typeof useRuntimeConsole>;

type DashboardResults = {
  health: PromiseSettledResult<HealthResponse>;
  session: PromiseSettledResult<SessionResponse>;
  models: PromiseSettledResult<ModelResponse>;
  providers: PromiseSettledResult<ProviderStatusResponse>;
  providerPresets: PromiseSettledResult<{ object: string; data: ProviderPresetRecord[] }>;
  budget: PromiseSettledResult<BudgetStatusResponse>;
  accountSummary: PromiseSettledResult<AccountSummaryResponse>;
  chatSessions: PromiseSettledResult<ChatSessionsResponse>;
  requestLedger: PromiseSettledResult<RequestLedgerResponse>;
  adminConfig: PromiseSettledResult<ConfiguredStateResponse>;
  retentionRuns: PromiseSettledResult<{ object: string; data: RetentionRunData[] }>;
};

type DashboardPreviousState = {
  providers: ProviderStatusResponse["data"];
  budget: BudgetStatusResponse["data"] | null;
  accountSummary: AccountSummaryResponse["data"] | null;
  chatSessions: ChatSessionsResponse["data"];
  activeChatSession: ChatSessionRecord | null;
  requestLedger: RequestLedgerResponse["data"];
  adminConfig: ConfiguredStateResponse["data"] | null;
  retentionRuns: RetentionRunData[];
  retentionLastRun: RetentionRunData | null;
};

type DashboardSnapshot = {
  health: HealthResponse;
  sessionInfo: SessionResponse["data"] | null;
  models: ModelResponse["data"];
  providers: ProviderStatusResponse["data"];
  providerPresets: ProviderPresetRecord[];
  budget: BudgetStatusResponse["data"] | null;
  accountSummary: AccountSummaryResponse["data"] | null;
  chatSessions: ChatSessionsResponse["data"];
  chatSessionsHasMore: boolean;
  activeChatSessionID: string;
  activeChatSession: ChatSessionRecord | null;
  requestLedger: RequestLedgerResponse["data"];
  adminConfig: ConfiguredStateResponse["data"] | null;
  retentionRuns: RetentionRunData[];
  retentionLastRun: RetentionRunData | null;
};

async function resolveDashboardSnapshot(args: {
  authToken: string;
  activeChatSessionID: string;
  previous: DashboardPreviousState;
}): Promise<DashboardSnapshot> {
  const results = await loadDashboardResults(args.authToken);
  const health = requireFulfilledDashboardResult(results.health);
  const sessionInfo = results.session.status === "fulfilled" ? results.session.value.data : null;
  const models = resolveModelsResult(results.models);
  const providers = resolveAuthorizedDashboardResult(results.providers, {
    unauthorized: [],
    other: args.previous.providers,
  });
  const providerPresets = results.providerPresets.status === "fulfilled" ? results.providerPresets.value.data : [];
  const budget = resolveAuthorizedDashboardResult(results.budget, {
    unauthorized: null,
    other: args.previous.budget,
  });
  const accountSummary = resolveAuthorizedDashboardResult(results.accountSummary, {
    unauthorized: null,
    other: args.previous.accountSummary,
  });
  const requestLedger = resolveAuthorizedDashboardResult(results.requestLedger, {
    unauthorized: [],
    other: args.previous.requestLedger,
  });
  const adminConfig = resolveAuthorizedDashboardResult(results.adminConfig, {
    unauthorized: null,
    other: args.previous.adminConfig,
  });
  const retentionRuns = resolveAuthorizedDashboardResult(results.retentionRuns, {
    unauthorized: [],
    other: args.previous.retentionRuns,
  });
  const retentionLastRun = retentionRuns[0] ?? null;
  const chatState = await resolveChatDashboardState({
    authToken: args.authToken,
    activeChatSessionID: args.activeChatSessionID,
    previousSessions: args.previous.chatSessions,
    previousActiveSession: args.previous.activeChatSession,
    result: results.chatSessions,
  });

  return {
    health,
    sessionInfo,
    models,
    providers,
    providerPresets,
    budget,
    accountSummary,
    chatSessions: chatState.sessions,
    chatSessionsHasMore: chatState.hasMore,
    activeChatSessionID: chatState.activeChatSessionID,
    activeChatSession: chatState.activeChatSession,
    requestLedger,
    adminConfig,
    retentionRuns,
    retentionLastRun,
  };
}

async function loadDashboardResults(authToken: string): Promise<DashboardResults> {
  const [
    health,
    session,
    models,
    providers,
    providerPresets,
    budget,
    accountSummary,
    chatSessions,
    requestLedger,
    adminConfig,
    retentionRuns,
  ] = await Promise.allSettled([
    getHealth(),
    getSession(authToken),
    getModels(authToken),
    getProviders(authToken),
    getProviderPresets(authToken),
    getBudget("", authToken),
    getAccountSummary("", authToken),
    getChatSessions(authToken, 20),
    getRequestLedger(authToken, 20),
    getAdminConfig(authToken),
    getRetentionRuns(authToken, 10),
  ]);

  return {
    health,
    session,
    models,
    providers,
    providerPresets,
    budget,
    accountSummary,
    chatSessions,
    requestLedger,
    adminConfig,
    retentionRuns,
  };
}

function requireFulfilledDashboardResult<T>(result: PromiseSettledResult<T>): T {
  if (result.status === "fulfilled") {
    return result.value;
  }
  throw new Error("failed to load runtime console data");
}

function resolveModelsResult(result: PromiseSettledResult<ModelResponse>): ModelResponse["data"] {
  if (result.status === "fulfilled") {
    return result.value.data;
  }
  if (isInvalidBearerTokenError(result.reason)) {
    return [];
  }
  throw new Error("failed to load runtime console data");
}

function resolveAuthorizedDashboardResult<T>(
  result: PromiseSettledResult<{ data: T }>,
  fallbacks: { unauthorized: T; other: T },
): T {
  if (result.status === "fulfilled") {
    return result.value.data;
  }
  if (isInvalidBearerTokenError(result.reason)) {
    return fallbacks.unauthorized;
  }
  return fallbacks.other;
}

async function resolveChatDashboardState(args: {
  authToken: string;
  activeChatSessionID: string;
  previousSessions: ChatSessionsResponse["data"];
  previousActiveSession: ChatSessionRecord | null;
  result: PromiseSettledResult<ChatSessionsResponse>;
}): Promise<{
  sessions: ChatSessionsResponse["data"];
  hasMore: boolean;
  activeChatSessionID: string;
  activeChatSession: ChatSessionRecord | null;
}> {
  if (args.result.status !== "fulfilled") {
    if (isInvalidBearerTokenError(args.result.reason)) {
      return {
        sessions: [],
        hasMore: false,
        activeChatSessionID: "",
        activeChatSession: null,
      };
    }
    return {
      sessions: args.previousSessions,
      hasMore: false,
      activeChatSessionID: args.activeChatSessionID,
      activeChatSession: args.previousActiveSession,
    };
  }

  const sessions = args.result.value.data ?? [];
  const hasMore = args.result.value.has_more ?? false;
  const activeChatSessionID = sessions.some((entry) => entry.id === args.activeChatSessionID)
    ? args.activeChatSessionID
    : sessions[0]?.id ?? "";

  if (!activeChatSessionID) {
    return {
      sessions,
      hasMore,
      activeChatSessionID,
      activeChatSession: null,
    };
  }

  try {
    const sessionResult = await getChatSession(activeChatSessionID, args.authToken);
    return {
      sessions,
      hasMore,
      activeChatSessionID,
      activeChatSession: sessionResult.data,
    };
  } catch {
    return {
      sessions,
      hasMore,
      activeChatSessionID,
      activeChatSession: null,
    };
  }
}

function isInvalidBearerTokenError(error: unknown): boolean {
  return error instanceof Error && error.message === invalidBearerTokenMessage;
}

function deriveSessionState(sessionInfo: SessionResponse["data"] | null): SessionState {
  const role = sessionInfo?.role ?? "anonymous";
  const authDisabled = sessionInfo?.source === "auth_disabled";
  const kind: SessionKind = sessionInfo?.invalid_token
    ? "invalid"
    : role === "admin" || authDisabled
      ? "admin"
      : sessionInfo?.authenticated
        ? "tenant"
        : "anonymous";

  const label =
    kind === "admin"
      ? "Admin"
      : kind === "tenant"
        ? `Tenant${sessionInfo?.tenant ? `: ${sessionInfo.tenant}` : ""}`
        : kind === "invalid"
          ? "Invalid token"
          : "Anonymous";

  const capabilities =
    kind === "admin"
      ? ["Playground access", "Model catalog", "Provider status", "Budget admin", "Control-plane admin"]
      : kind === "tenant"
        ? ["Playground access", "Model catalog"]
        : kind === "anonymous"
          ? ["Health view", "Authentication setup"]
          : ["No confirmed access"];

  return {
    kind,
    label,
    capabilities,
    isAdmin: kind === "admin",
    isAuthenticated: kind === "admin" || kind === "tenant",
    role,
    name: sessionInfo?.name ?? "",
    tenant: sessionInfo?.tenant ?? "",
    source: sessionInfo?.source ?? "",
    keyID: sessionInfo?.key_id ?? "",
    allowedProviders: sessionInfo?.allowed_providers ?? [],
    allowedModels: sessionInfo?.allowed_models ?? [],
  };
}
