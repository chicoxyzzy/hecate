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
  deleteProvider as deleteProviderRequest,
  deleteTenant as deleteTenantRequest,
  getAccountSummary,
  getBudget,
  getChatSession,
  getChatSessions,
  getControlPlane,
  getHealth,
  getModels,
  getProviderPresets,
  getProviders,
  getRequestLedger,
  getRetentionRuns,
  getSession,
  getTrace,
  rotateAPIKey as rotateAPIKeyRequest,
  rotateProviderSecret as rotateProviderSecretRequest,
  runRetention as runRetentionRequest,
  resetBudget as resetBudgetRequest,
  setAPIKeyEnabled as setAPIKeyEnabledRequest,
  setBudgetLimit as setBudgetLimitRequest,
  setProviderEnabled as setProviderEnabledRequest,
  setTenantEnabled as setTenantEnabledRequest,
  topUpBudget as topUpBudgetRequest,
  upsertAPIKey as upsertAPIKeyRequest,
  upsertProvider as upsertProviderRequest,
  upsertTenant as upsertTenantRequest,
} from "../lib/api";
import type {
  BudgetStatusResponse,
  AccountSummaryResponse,
  ChatResponse,
  ChatSessionRecord,
  ChatSessionsResponse,
  ControlPlaneResponse,
  HealthResponse,
  ModelFilter,
  ModelResponse,
  ProviderPresetRecord,
  ProviderFilter,
  ProviderStatusResponse,
  RequestLedgerResponse,
  RuntimeHeaders,
  SessionResponse,
  TraceResponse,
  TraceSpanRecord,
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

export function useRuntimeConsole() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [models, setModels] = useState<ModelResponse["data"]>([]);
  const [providers, setProviders] = useState<ProviderStatusResponse["data"]>([]);
  const [providerPresets, setProviderPresets] = useState<ProviderPresetRecord[]>([]);
  const [budget, setBudget] = useState<BudgetStatusResponse["data"] | null>(null);
  const [accountSummary, setAccountSummary] = useState<AccountSummaryResponse["data"] | null>(null);
  const [requestLedger, setRequestLedger] = useState<RequestLedgerResponse["data"]>([]);
  const [controlPlane, setControlPlane] = useState<ControlPlaneResponse["data"] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [model, setModel] = useState("");
  const [tenant, setTenant] = useState("team-a");
  const [message, setMessage] = useState(defaultPrompt);
  const [chatLoading, setChatLoading] = useState(false);
  const [streamingContent, setStreamingContent] = useState<string | null>(null);
  const [chatResult, setChatResult] = useState<ChatResponse | null>(null);
  // pendingToolCalls: model responded with tool_calls; waiting for user to fill results.
  const [pendingToolCalls, setPendingToolCalls] = useState<Array<{ id: string; name: string; arguments: string; result: string }>>([]);
  // Thread of messages that preceded the pending tool calls (history + user message + assistant tool_calls message).
  const [pendingThread, setPendingThread] = useState<import("../lib/api").ChatMessage[] | null>(null);
  const [chatSessions, setChatSessions] = useState<ChatSessionsResponse["data"]>([]);
  const [activeChatSessionID, setActiveChatSessionID] = useState("");
  const [activeChatSession, setActiveChatSession] = useState<ChatSessionRecord | null>(null);
  const [runtimeHeaders, setRuntimeHeaders] = useState<RuntimeHeaders | null>(null);
  const [traceSpans, setTraceSpans] = useState<TraceSpanRecord[]>([]);
  const [traceRoute, setTraceRoute] = useState<TraceResponse["data"]["route"] | null>(null);
  const [traceStartedAt, setTraceStartedAt] = useState("");
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceError, setTraceError] = useState("");
  const [chatError, setChatError] = useState("");
  const [modelFilter, setModelFilter] = useState<ModelFilter>("all");
  const [providerFilter, setProviderFilter] = useState<ProviderFilter>("auto");
  const [copiedCommand, setCopiedCommand] = useState("");

  const [budgetAmountUsd, setBudgetAmountUsd] = useState("1.00");
  const [budgetLimitUsd, setBudgetLimitUsd] = useState("5.00");
  const [budgetActionError, setBudgetActionError] = useState("");

  const [authToken, setAuthToken] = useState("");
  const [sessionInfo, setSessionInfo] = useState<SessionResponse["data"] | null>(null);
  const [controlPlaneError, setControlPlaneError] = useState("");
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
  const [providerFormID, setProviderFormID] = useState("");
  const [providerFormName, setProviderFormName] = useState("");
  const [providerFormKind, setProviderFormKind] = useState("cloud");
  const [providerFormProtocol, setProviderFormProtocol] = useState("openai");
  const [providerFormBaseURL, setProviderFormBaseURL] = useState("");
  const [providerFormAPIVersion, setProviderFormAPIVersion] = useState("");
  const [providerFormDefaultModel, setProviderFormDefaultModel] = useState("");
  const [providerFormEnabled, setProviderFormEnabled] = useState("true");
  const [providerFormSecret, setProviderFormSecret] = useState("");
  const [providerFormPresetID, setProviderFormPresetID] = useState("");
  const [rotateProviderID, setRotateProviderID] = useState("");
  const [rotateProviderSecret, setRotateProviderSecret] = useState("");
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
    const storedAuthToken = window.localStorage.getItem("hecate.authToken");
    if (storedAuthToken) {
      setAuthToken(storedAuthToken);
    }
    const storedChatSessionID = window.localStorage.getItem("hecate.chatSessionID");
    if (storedChatSessionID) {
      setActiveChatSessionID(storedChatSessionID);
    }
  }, []);

  useEffect(() => {
    void loadDashboard();
  }, [authToken]);

  useEffect(() => {
    window.localStorage.setItem("hecate.authToken", authToken);
  }, [authToken]);

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
      if (model !== "") {
        setModel("");
      }
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

  async function loadDashboard() {
    setLoading(true);
    setError("");
    setControlPlaneError("");

    try {
      const [healthResult, sessionResult, modelsResult, providersResult, providerPresetsResult, budgetResult, accountSummaryResult, chatSessionsResult, requestLedgerResult, controlPlaneResult, retentionRunsResult] = await Promise.allSettled([
        getHealth(),
        getSession(authToken),
        getModels(authToken),
        getProviders(authToken),
        getProviderPresets(authToken),
        getBudget("", authToken),
        getAccountSummary("", authToken),
        getChatSessions(authToken, 20),
        getRequestLedger(authToken, 20),
        getControlPlane(authToken),
        getRetentionRuns(authToken, 10),
      ]);

      if (healthResult.status !== "fulfilled") {
        throw new Error("failed to load runtime console data");
      }

      setHealth(healthResult.value);
      if (sessionResult.status === "fulfilled") {
        setSessionInfo(sessionResult.value.data);
      } else {
        setSessionInfo(null);
      }
      if (modelsResult.status === "fulfilled") {
        setModels(modelsResult.value.data);
      } else if (modelsResult.reason instanceof Error && modelsResult.reason.message === "missing or invalid bearer token") {
        setModels([]);
      } else {
        throw new Error("failed to load runtime console data");
      }

      if (providersResult.status === "fulfilled") {
        setProviders(providersResult.value.data);
      } else if (providersResult.reason instanceof Error && providersResult.reason.message === "missing or invalid bearer token") {
        setProviders([]);
      }

      if (providerPresetsResult.status === "fulfilled") {
        setProviderPresets(providerPresetsResult.value.data);
      } else {
        setProviderPresets([]);
      }

      if (budgetResult.status === "fulfilled") {
        setBudget(budgetResult.value.data);
      } else if (budgetResult.reason instanceof Error && budgetResult.reason.message === "missing or invalid bearer token") {
        setBudget(null);
      }

      if (accountSummaryResult.status === "fulfilled") {
        setAccountSummary(accountSummaryResult.value.data);
      } else if (accountSummaryResult.reason instanceof Error && accountSummaryResult.reason.message === "missing or invalid bearer token") {
        setAccountSummary(null);
      }

      if (chatSessionsResult.status === "fulfilled") {
        const sessions = chatSessionsResult.value.data ?? [];
        setChatSessions(sessions);
        const selectedSessionID = sessions.some((entry) => entry.id === activeChatSessionID) ? activeChatSessionID : sessions[0]?.id ?? "";
        setActiveChatSessionID(selectedSessionID);
        if (selectedSessionID) {
          try {
            const sessionResult = await getChatSession(selectedSessionID, authToken);
            setActiveChatSession(sessionResult.data);
          } catch {
            setActiveChatSession(null);
          }
        } else {
          setActiveChatSession(null);
        }
      } else if (chatSessionsResult.reason instanceof Error && chatSessionsResult.reason.message === "missing or invalid bearer token") {
        setChatSessions([]);
        setActiveChatSession(null);
        setActiveChatSessionID("");
      }

      if (requestLedgerResult.status === "fulfilled") {
        setRequestLedger(requestLedgerResult.value.data ?? []);
      } else if (requestLedgerResult.reason instanceof Error && requestLedgerResult.reason.message === "missing or invalid bearer token") {
        setRequestLedger([]);
      }

      if (controlPlaneResult.status === "fulfilled") {
        setControlPlane(controlPlaneResult.value.data);
      } else if (controlPlaneResult.reason instanceof Error && controlPlaneResult.reason.message === "missing or invalid bearer token") {
        setControlPlane(null);
      }

      if (retentionRunsResult.status === "fulfilled") {
        setRetentionRuns(retentionRunsResult.value.data);
        setRetentionLastRun(retentionRunsResult.value.data[0] ?? null);
      } else if (retentionRunsResult.reason instanceof Error && retentionRunsResult.reason.message === "missing or invalid bearer token") {
        setRetentionRuns([]);
        setRetentionLastRun(null);
      }
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
    setTraceError("");

    try {
      let sessionID = activeChatSessionID;
      if (!sessionID) {
        const createdSession = await createChatSessionRequest(
          {
            title: deriveChatSessionTitle(message),
          },
          authToken,
        );
        sessionID = createdSession.data.id;
        setActiveChatSessionID(sessionID);
        setActiveChatSession(createdSession.data);
        setChatSessions((current) => [renderChatSessionSummary(createdSession.data), ...current.filter((entry) => entry.id !== createdSession.data.id)]);
      }

      const messages = buildMessagesForSubmission(activeChatSession, message);
      const chatPayload = {
        model,
        provider: providerFilter === "auto" ? "" : providerFilter,
        session_id: sessionID,
        user: tenant,
        messages,
      };
      setPendingToolCalls([]);
      setPendingThread(null);

      const chatExecution = await executeChatRequest(chatPayload, chatPayload.messages);
      if (chatExecution.kind === "tool_calls") {
        return;
      }
      const { headers } = chatExecution;

      setChatResult(chatExecution.chatResult);
      setMessage("");
      await refreshTrace(headers.requestId, true);

      try {
        const scopedBudget = await getBudget(
          `?scope=tenant_provider&tenant=${encodeURIComponent(tenant)}&provider=${encodeURIComponent(headers.provider)}`,
          authToken,
        );
        setBudget(scopedBudget.data);
      } catch {
        // Tenant-key users may not be authorized for admin budget views.
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

      if (session.isAdmin) {
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
    const chatPayload = {
      model,
      provider: providerFilter === "auto" ? "" : providerFilter,
      session_id: activeChatSessionID || undefined,
      user: tenant,
      messages,
    };

    try {
      const chatExecution = await executeChatRequest(chatPayload, messages);
      if (chatExecution.kind === "tool_calls") {
        return;
      }

      setPendingToolCalls([]);
      setPendingThread(null);
      setChatResult(chatExecution.chatResult);
      await refreshTrace(chatExecution.headers.requestId, false);
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
    setStreamingContent(null);
    setRuntimeHeaders(response.headers);

    if (response.finishReason === "tool_calls" && response.toolCalls.length > 0) {
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

  async function refreshTrace(requestID: string, reportErrors: boolean) {
    setTraceLoading(true);
    try {
      const trace = await getTrace(requestID, authToken);
      setTraceSpans(trace.data.spans ?? []);
      setTraceRoute(trace.data.route ?? null);
      setTraceStartedAt(trace.data.started_at ?? "");
      if (reportErrors) {
        setTraceError("");
      }
    } catch (traceLoadError) {
      setTraceSpans([]);
      setTraceRoute(null);
      setTraceStartedAt("");
      if (reportErrors) {
        setTraceError(traceLoadError instanceof Error ? traceLoadError.message : "failed to load trace");
      }
    } finally {
      setTraceLoading(false);
    }
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
    setNotice({ kind, message });
  }

  function describeError(error: unknown, fallback: string): string {
    return error instanceof Error ? error.message : fallback;
  }

  function resetControlPlaneFeedback() {
    setControlPlaneError("");
    setNotice(null);
  }

  async function runControlPlaneMutation(options: {
    action: () => Promise<void>;
    successMessage: string;
    errorMessage: string;
    failureDetail: string;
  }) {
    resetControlPlaneFeedback();
    try {
      await options.action();
      await loadDashboard();
      setNoticeMessage("success", options.successMessage);
    } catch (error) {
      setControlPlaneError(describeError(error, options.failureDetail));
      setNoticeMessage("error", options.errorMessage);
    }
  }

  async function upsertTenant() {
    await runControlPlaneMutation({
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
        setTenantFormID("");
        setTenantFormName("");
        setTenantFormProviders("");
        setTenantFormModels("");
      },
    });
  }

  async function upsertAPIKey() {
    await runControlPlaneMutation({
      successMessage: "API key saved.",
      errorMessage: "Failed to save API key.",
      failureDetail: "failed to save api key",
      action: async () => {
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
        setAPIKeyFormID("");
        setAPIKeyFormName("");
        setAPIKeyFormSecret("");
        setAPIKeyFormTenant("");
        setAPIKeyFormProviders("");
        setAPIKeyFormModels("");
      },
    });
  }

  function populateProviderFormFromPreset(presetID: string) {
    setProviderFormPresetID(presetID);
    const preset = providerPresets.find((entry) => entry.id === presetID);
    if (!preset) {
      return;
    }
    setProviderFormID(preset.id);
    setProviderFormName(preset.id);
    setProviderFormKind(preset.kind);
    setProviderFormProtocol(preset.protocol);
    setProviderFormBaseURL(preset.base_url);
    setProviderFormAPIVersion(preset.api_version ?? "");
    setProviderFormDefaultModel("");
    setProviderFormEnabled("true");
    setProviderFormSecret("");
  }

  async function upsertProvider() {
    await runControlPlaneMutation({
      successMessage: "Provider saved.",
      errorMessage: "Failed to save provider.",
      failureDetail: "failed to save provider",
      action: async () => {
        const payload = buildProviderUpsertPayload({
        presetID: providerFormPresetID,
        id: providerFormID,
        name: providerFormName,
        kind: providerFormKind,
        protocol: providerFormProtocol,
        baseURL: providerFormBaseURL,
        apiVersion: providerFormAPIVersion,
        defaultModel: providerFormDefaultModel,
        enabled: providerFormEnabled === "true",
        key: providerFormSecret,
        presets: providerPresets,
      });
        await upsertProviderRequest(
        payload,
        authToken,
      );
        setProviderFormSecret("");
      },
    });
  }

  async function setProviderEnabled(id: string, enabled: boolean) {
    await runControlPlaneMutation({
      successMessage: `Provider ${enabled ? "enabled" : "disabled"}.`,
      errorMessage: "Failed to update provider state.",
      failureDetail: "failed to update provider state",
      action: async () => {
        await setProviderEnabledRequest({ id, enabled }, authToken);
      },
    });
  }

  async function rotateProviderCredential() {
    await runControlPlaneMutation({
      successMessage: "Provider secret rotated.",
      errorMessage: "Failed to rotate provider secret.",
      failureDetail: "failed to rotate provider secret",
      action: async () => {
        await rotateProviderSecretRequest({ id: rotateProviderID, key: rotateProviderSecret }, authToken);
        setRotateProviderID("");
        setRotateProviderSecret("");
      },
    });
  }

  async function deleteProvider(id: string) {
    resetControlPlaneFeedback();
    if (!window.confirm(`Delete provider "${id}"? This cannot be undone.`)) {
      return;
    }
    await runControlPlaneMutation({
      successMessage: "Provider deleted.",
      errorMessage: "Failed to delete provider.",
      failureDetail: "failed to delete provider",
      action: async () => {
        await deleteProviderRequest({ id }, authToken);
      },
    });
  }

  async function setTenantEnabled(id: string, enabled: boolean) {
    await runControlPlaneMutation({
      successMessage: `Tenant ${enabled ? "enabled" : "disabled"}.`,
      errorMessage: "Failed to update tenant state.",
      failureDetail: "failed to update tenant state",
      action: async () => {
        await setTenantEnabledRequest({ id, enabled }, authToken);
      },
    });
  }

  async function deleteTenant(id: string) {
    resetControlPlaneFeedback();
    if (!window.confirm(`Delete tenant "${id}"? This cannot be undone.`)) {
      return;
    }
    await runControlPlaneMutation({
      successMessage: "Tenant deleted.",
      errorMessage: "Failed to delete tenant.",
      failureDetail: "failed to delete tenant",
      action: async () => {
        await deleteTenantRequest({ id }, authToken);
      },
    });
  }

  async function setAPIKeyEnabled(id: string, enabled: boolean) {
    await runControlPlaneMutation({
      successMessage: `API key ${enabled ? "enabled" : "disabled"}.`,
      errorMessage: "Failed to update API key state.",
      failureDetail: "failed to update api key state",
      action: async () => {
        await setAPIKeyEnabledRequest({ id, enabled }, authToken);
      },
    });
  }

  async function rotateAPIKey() {
    await runControlPlaneMutation({
      successMessage: "API key rotated.",
      errorMessage: "Failed to rotate API key.",
      failureDetail: "failed to rotate api key",
      action: async () => {
        await rotateAPIKeyRequest({ id: rotateAPIKeyID, key: rotateAPIKeySecret }, authToken);
        setRotateAPIKeyID("");
        setRotateAPIKeySecret("");
      },
    });
  }

  async function deleteAPIKey(id: string) {
    resetControlPlaneFeedback();
    if (!window.confirm(`Delete API key "${id}"? This cannot be undone.`)) {
      return;
    }
    await runControlPlaneMutation({
      successMessage: "API key deleted.",
      errorMessage: "Failed to delete API key.",
      failureDetail: "failed to delete api key",
      action: async () => {
        await deleteAPIKeyRequest({ id }, authToken);
      },
    });
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

  async function createChatSession() {
    setNotice(null);
    try {
      const payload = await createChatSessionRequest(
        {
          title: deriveChatSessionTitle(message),
        },
        authToken,
      );
      setActiveChatSessionID(payload.data.id);
      setActiveChatSession(payload.data);
      setChatSessions((current) => [renderChatSessionSummary(payload.data), ...current.filter((entry) => entry.id !== payload.data.id)]);
      setNotice({ kind: "success", message: "Chat session created." });
    } catch (error) {
      setChatError(error instanceof Error ? error.message : "failed to create chat session");
      setNotice({ kind: "error", message: "Failed to create chat session." });
    }
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
    setChatResult(null);
    setRuntimeHeaders(null);
    setTraceSpans([]);
    setTraceRoute(null);
    setTraceStartedAt("");
    setChatError("");
    setTraceError("");
  }

  async function deleteChatSession(id: string) {
    try {
      await deleteChatSessionRequest(id, authToken);
      setChatSessions((current) => current.filter((s) => s.id !== id));
      if (activeChatSessionID === id) {
        startNewChat();
      }
      setNotice({ kind: "success", message: "Session deleted." });
    } catch (error) {
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "Failed to delete session." });
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
      setNotice({ kind: "error", message: error instanceof Error ? error.message : "Failed to rename session." });
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
      controlPlane,
      controlPlaneError,
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
      model,
      modelFilter,
      models,
      notice,
      session,
      providerFilter,
      providerScopedModels,
      providerFormAPIVersion,
      providerFormBaseURL,
      providerFormDefaultModel,
      providerFormEnabled,
      providerFormID,
      providerFormKind,
      providerFormName,
      providerFormPresetID,
      providerFormProtocol,
      providerFormSecret,
      providers,
      providerPresets,
      rotateProviderID,
      rotateProviderSecret,
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
      traceError,
      traceSpans,
      traceLoading,
      traceRoute,
      traceStartedAt,
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
      deleteProvider,
      deleteTenant,
      createChatSession,
      deleteChatSession,
      renameChatSession,
      loadDashboard,
      resetBudget,
      rotateAPIKey,
      rotateProviderCredential,
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
      setModel,
      setModelFilter,
      setProviderFilter: selectProviderRoute,
      setProviderEnabled,
      setProviderFormAPIVersion,
      setProviderFormBaseURL,
      setProviderFormDefaultModel,
      setProviderFormEnabled,
      setProviderFormID,
      setProviderFormKind,
      setProviderFormName,
      setProviderFormPresetID,
      setProviderFormProtocol,
      setProviderFormSecret,
      setRetentionSubsystems,
      setRotateAPIKeyID,
      setRotateAPIKeySecret,
      setRotateProviderID,
      setRotateProviderSecret,
      setTenantEnabled,
      setTenant,
      setTenantFormID,
      setTenantFormModels,
      setTenantFormName,
      setTenantFormProviders,
      setBudgetLimit,
      populateProviderFormFromPreset,
      runRetention,
      selectChatSession,
      startNewChat,
      submitChat,
      submitToolResults,
      updateToolResult,
      topUpBudget,
      upsertAPIKey,
      upsertProvider,
      upsertTenant,
      clearAuthToken: () => setAuthToken(""),
      dismissNotice: () => setNotice(null),
    },
  };
}

function buildProviderUpsertPayload(args: {
  presetID: string;
  id: string;
  name: string;
  kind: string;
  protocol: string;
  baseURL: string;
  apiVersion: string;
  defaultModel: string;
  enabled: boolean;
  key: string;
  presets: ProviderPresetRecord[];
}) {
  const payload: {
    id: string;
    name: string;
    preset_id?: string;
    kind?: string;
    protocol?: string;
    base_url?: string;
    api_version?: string;
    default_model?: string;
    enabled: boolean;
    key: string;
  } = {
    id: args.id,
    name: args.name,
    enabled: args.enabled,
    key: args.key,
  };

  const preset = args.presets.find((entry) => entry.id === args.presetID);
  if (!preset) {
    payload.kind = args.kind;
    payload.protocol = args.protocol;
    payload.base_url = args.baseURL;
    if (args.apiVersion) {
      payload.api_version = args.apiVersion;
    }
    if (args.defaultModel) {
      payload.default_model = args.defaultModel;
    }
    return payload;
  }

  payload.preset_id = preset.id;
  if (args.kind && args.kind !== preset.kind) {
    payload.kind = args.kind;
  }
  if (args.protocol && args.protocol !== preset.protocol) {
    payload.protocol = args.protocol;
  }
  if (args.baseURL && args.baseURL !== preset.base_url) {
    payload.base_url = args.baseURL;
  }
  if (args.apiVersion && args.apiVersion !== (preset.api_version ?? "")) {
    payload.api_version = args.apiVersion;
  }
  if (args.defaultModel) {
    payload.default_model = args.defaultModel;
  }
  return payload;
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

function buildMessagesForSubmission(activeSession: ChatSessionRecord | null, message: string): ChatMessage[] {
  const history: ChatMessage[] =
    activeSession?.turns?.flatMap((turn) => {
      const user: ChatMessage = { role: "user", content: turn.user_message.content ?? "" };
      const assistant: ChatMessage = turn.assistant_message.tool_calls?.length
        ? { role: "assistant", content: turn.assistant_message.content ?? null, tool_calls: turn.assistant_message.tool_calls }
        : { role: "assistant", content: turn.assistant_message.content ?? "" };
      return [user, assistant];
    }) ?? [];
  return [...history, { role: "user", content: message }];
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

function deriveSessionState(sessionInfo: SessionResponse["data"] | null): SessionState {
  const role = sessionInfo?.role ?? "anonymous";
  const kind: SessionKind = sessionInfo?.invalid_token
    ? "invalid"
    : role === "admin"
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
