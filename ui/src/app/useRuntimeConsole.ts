import { useEffect, useMemo, useState, type SyntheticEvent } from "react";

import { buildLocalProviderIssue } from "../lib/provider-issues";
import type { LocalProviderIssue } from "../lib/provider-issues";
import { filterModelsByKind, filterModelsByProvider, parseCSV, usdToMicros } from "../lib/runtime-utils";
import {
  chatCompletions,
  createChatSession as createChatSessionRequest,
  deleteChatSession as deleteChatSessionRequest,
  updateChatSession as updateChatSessionRequest,
  deleteAPIKey as deleteAPIKeyRequest,
  deleteTenant as deleteTenantRequest,
  getAccountSummary,
  getBudget,
  getChatSession,
  getChatSessions,
  getControlPlane,
  getHealth,
  getModels,
  getProviders,
  getRequestLedger,
  getRetentionRuns,
  getSession,
  getTrace,
  rotateAPIKey as rotateAPIKeyRequest,
  runRetention as runRetentionRequest,
  resetBudget as resetBudgetRequest,
  setAPIKeyEnabled as setAPIKeyEnabledRequest,
  setBudgetLimit as setBudgetLimitRequest,
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
  ControlPlaneResponse,
  HealthResponse,
  ModelFilter,
  ModelResponse,
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
  const [budget, setBudget] = useState<BudgetStatusResponse["data"] | null>(null);
  const [accountSummary, setAccountSummary] = useState<AccountSummaryResponse["data"] | null>(null);
  const [requestLedger, setRequestLedger] = useState<RequestLedgerResponse["data"]>([]);
  const [controlPlane, setControlPlane] = useState<ControlPlaneResponse["data"] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [model, setModel] = useState("gpt-4o-mini");
  const [tenant, setTenant] = useState("team-a");
  const [message, setMessage] = useState(defaultPrompt);
  const [chatLoading, setChatLoading] = useState(false);
  const [chatResult, setChatResult] = useState<ChatResponse | null>(null);
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
      return;
    }
    const stillValid = models.some((entry) => entry.id === model && entry.metadata?.provider === providerFilter);
    if (stillValid) {
      return;
    }
    const nextModel = models.find((entry) => entry.metadata?.provider === providerFilter)?.id ?? "";
    setModel(nextModel);
  }, [model, models, providerFilter]);

  useEffect(() => {
    if (session.kind !== "tenant" || !session.tenant) {
      return;
    }
    setTenant((current) => (current === session.tenant ? current : session.tenant));
  }, [session.kind, session.tenant]);

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
      const [healthResult, sessionResult, modelsResult, providersResult, budgetResult, accountSummaryResult, chatSessionsResult, requestLedgerResult, controlPlaneResult, retentionRunsResult] = await Promise.allSettled([
        getHealth(),
        getSession(authToken),
        getModels(authToken),
        getProviders(authToken),
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
      const response = await chatCompletions(
        {
          model,
          provider: providerFilter === "auto" ? "" : providerFilter,
          session_id: sessionID,
          user: tenant,
          messages,
        },
        authToken,
      );

      setChatResult(response.data);
      setRuntimeHeaders(response.headers);
      setMessage("");
      setTraceLoading(true);
      try {
        const trace = await getTrace(response.headers.requestId, authToken);
        setTraceSpans(trace.data.spans ?? []);
        setTraceRoute(trace.data.route ?? null);
        setTraceStartedAt(trace.data.started_at ?? "");
      } catch (traceLoadError) {
        setTraceSpans([]);
        setTraceRoute(null);
        setTraceStartedAt("");
        setTraceError(traceLoadError instanceof Error ? traceLoadError.message : "failed to load trace");
      } finally {
        setTraceLoading(false);
      }

      try {
        const scopedBudget = await getBudget(
          `?scope=tenant_provider&tenant=${encodeURIComponent(tenant)}&provider=${encodeURIComponent(response.headers.provider)}`,
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

  async function upsertTenant() {
    setControlPlaneError("");
    setNotice(null);
    try {
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
      await loadDashboard();
      setNotice({ kind: "success", message: "Tenant saved." });
    } catch (error) {
      setControlPlaneError(error instanceof Error ? error.message : "failed to save tenant");
      setNotice({ kind: "error", message: "Failed to save tenant." });
    }
  }

  async function upsertAPIKey() {
    setControlPlaneError("");
    setNotice(null);
    try {
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
      await loadDashboard();
      setNotice({ kind: "success", message: "API key saved." });
    } catch (error) {
      setControlPlaneError(error instanceof Error ? error.message : "failed to save api key");
      setNotice({ kind: "error", message: "Failed to save API key." });
    }
  }

  async function setTenantEnabled(id: string, enabled: boolean) {
    setControlPlaneError("");
    setNotice(null);
    try {
      await setTenantEnabledRequest({ id, enabled }, authToken);
      await loadDashboard();
      setNotice({ kind: "success", message: `Tenant ${enabled ? "enabled" : "disabled"}.` });
    } catch (error) {
      setControlPlaneError(error instanceof Error ? error.message : "failed to update tenant state");
      setNotice({ kind: "error", message: "Failed to update tenant state." });
    }
  }

  async function deleteTenant(id: string) {
    setControlPlaneError("");
    setNotice(null);
    if (!window.confirm(`Delete tenant "${id}"? This cannot be undone.`)) {
      return;
    }
    try {
      await deleteTenantRequest({ id }, authToken);
      await loadDashboard();
      setNotice({ kind: "success", message: "Tenant deleted." });
    } catch (error) {
      setControlPlaneError(error instanceof Error ? error.message : "failed to delete tenant");
      setNotice({ kind: "error", message: "Failed to delete tenant." });
    }
  }

  async function setAPIKeyEnabled(id: string, enabled: boolean) {
    setControlPlaneError("");
    setNotice(null);
    try {
      await setAPIKeyEnabledRequest({ id, enabled }, authToken);
      await loadDashboard();
      setNotice({ kind: "success", message: `API key ${enabled ? "enabled" : "disabled"}.` });
    } catch (error) {
      setControlPlaneError(error instanceof Error ? error.message : "failed to update api key state");
      setNotice({ kind: "error", message: "Failed to update API key state." });
    }
  }

  async function rotateAPIKey() {
    setControlPlaneError("");
    setNotice(null);
    try {
      await rotateAPIKeyRequest({ id: rotateAPIKeyID, key: rotateAPIKeySecret }, authToken);
      setRotateAPIKeyID("");
      setRotateAPIKeySecret("");
      await loadDashboard();
      setNotice({ kind: "success", message: "API key rotated." });
    } catch (error) {
      setControlPlaneError(error instanceof Error ? error.message : "failed to rotate api key");
      setNotice({ kind: "error", message: "Failed to rotate API key." });
    }
  }

  async function deleteAPIKey(id: string) {
    setControlPlaneError("");
    setNotice(null);
    if (!window.confirm(`Delete API key "${id}"? This cannot be undone.`)) {
      return;
    }
    try {
      await deleteAPIKeyRequest({ id }, authToken);
      await loadDashboard();
      setNotice({ kind: "success", message: "API key deleted." });
    } catch (error) {
      setControlPlaneError(error instanceof Error ? error.message : "failed to delete api key");
      setNotice({ kind: "error", message: "Failed to delete API key." });
    }
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
      chatResult,
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
      providers,
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
      setModel,
      setModelFilter,
      setProviderFilter,
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
      topUpBudget,
      upsertAPIKey,
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

function buildMessagesForSubmission(activeSession: ChatSessionRecord | null, message: string): Array<{ role: string; content: string }> {
  const history =
    activeSession?.turns?.flatMap((turn) => [
      { role: turn.user_message.role, content: turn.user_message.content },
      { role: turn.assistant_message.role, content: turn.assistant_message.content },
    ]) ?? [];
  return [...history, { role: "user", content: message }];
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
