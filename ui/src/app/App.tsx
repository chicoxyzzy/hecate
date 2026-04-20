import { useEffect, useMemo, useState } from "react";

import { AuthPanel } from "../components/AuthPanel";
import { BudgetPanel } from "../components/BudgetPanel";
import { ControlPlanePanel } from "../components/ControlPlanePanel";
import { LocalRuntimeIssues } from "../components/LocalRuntimeIssues";
import { ModelsPanel } from "../components/ModelsPanel";
import { PlaygroundPanel } from "../components/PlaygroundPanel";
import { ProviderHealthStrip } from "../components/ProviderHealthStrip";
import { ProvidersPanel } from "../components/ProvidersPanel";
import { SessionBadge } from "../components/SessionBadge";
import { StatCard } from "../components/StatCard";
import { useRuntimeConsole } from "./useRuntimeConsole";

const inputClassName =
  "w-full rounded-2xl border border-slate-200/80 bg-white/90 px-4 py-3 text-slate-900 outline-none transition focus:border-cyan-700 focus:ring-4 focus:ring-cyan-100";

type ViewID = "playground" | "providers" | "budgets" | "control-plane" | "auth";

export default function App() {
  const { state, actions } = useRuntimeConsole();
  const [activeView, setActiveView] = useState<ViewID>("playground");
  const views = useMemo(
    () =>
      [
        { id: "playground", label: "Playground" },
        { id: "providers", label: "Providers" },
        ...(state.session.isAdmin
          ? ([
              { id: "budgets", label: "Budgets" },
              { id: "control-plane", label: "Control Plane" },
            ] satisfies Array<{ id: ViewID; label: string }>)
          : []),
        { id: "auth", label: "Auth" },
      ] satisfies Array<{ id: ViewID; label: string }>,
    [state.session.isAdmin],
  );

  useEffect(() => {
    if (views.some((view) => view.id === activeView)) {
      return;
    }
    setActiveView("playground");
  }, [activeView, views]);

  return (
    <div className="min-h-screen bg-[radial-gradient(circle_at_top_left,rgba(255,198,120,0.35),transparent_28%),radial-gradient(circle_at_top_right,rgba(72,164,255,0.24),transparent_26%),linear-gradient(180deg,#f9f2e8_0%,#eef5f8_100%)] text-slate-900">
      <div className="mx-auto max-w-7xl px-5 py-8 md:px-6 md:py-10">
        <header className="mb-6 flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <p className="mb-1 text-xs font-semibold uppercase tracking-[0.22em] text-amber-700">AI Agent Runtime</p>
            <h1 className="font-serif text-5xl leading-none md:text-7xl">Hecate Console</h1>
            <p className="mt-3 max-w-3xl text-base text-slate-600 md:text-lg">
              A clearer operator console for authenticating, testing requests, inspecting providers, and managing runtime state.
            </p>
            <div className="mt-4 inline-flex flex-wrap items-center gap-2">
              <SessionBadge kind={state.session.kind} label={state.session.label} />
              <span className="rounded-full border border-slate-200/80 bg-white/75 px-3 py-2 text-sm text-slate-700">
                Gateway: {state.health?.status ?? (state.loading ? "loading" : "unknown")}
              </span>
            </div>
          </div>
          <button
            className="inline-flex rounded-full border border-slate-200/80 bg-white/75 px-4 py-3 text-sm font-medium text-slate-900 transition hover:-translate-y-0.5"
            onClick={() => void actions.loadDashboard()}
            type="button"
          >
            Refresh
          </button>
        </header>

        {state.error ? <div className="mb-4 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{state.error}</div> : null}
        {state.notice ? (
          <div
            className={
              state.notice.kind === "success"
                ? "mb-4 flex items-center justify-between gap-3 rounded-2xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800"
                : "mb-4 flex items-center justify-between gap-3 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700"
            }
          >
            <span>{state.notice.message}</span>
            <button
              className="rounded-full border border-current/20 px-3 py-1 text-xs font-medium"
              onClick={actions.dismissNotice}
              type="button"
            >
              Dismiss
            </button>
          </div>
        ) : null}

        <section className="mb-4 flex flex-wrap gap-2">
          {views.map((view) => (
            <button
              className={
                activeView === view.id
                  ? "rounded-full bg-slate-900 px-4 py-2.5 text-sm font-semibold text-white shadow-[0_12px_24px_rgba(15,23,42,0.18)]"
                  : "rounded-full border border-slate-200/80 bg-white/80 px-4 py-2.5 text-sm font-medium text-slate-700 transition hover:-translate-y-0.5"
              }
              key={view.id}
              onClick={() => setActiveView(view.id)}
              type="button"
            >
              {view.label}
            </button>
          ))}
        </section>

        <section className="mb-4 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <StatCard label="Providers Healthy" value={`${state.healthyProviders}/${state.providers.length || 0}`} />
          <StatCard label="Models Discovered" value={`${state.models.length} total`} />
          <StatCard label="Local Runtime" value={`${state.healthyLocalProviders}/${state.localProviders.length || 0} healthy`} />
          <StatCard label="Routing Posture" value={state.runtimeHeaders?.routeReason || "Awaiting request"} />
        </section>

        <main>
          {activeView === "playground" ? (
            <div className="grid gap-4 xl:grid-cols-[1.7fr_minmax(0,1fr)]">
              <PlaygroundPanel
                allowedModels={state.session.allowedModels}
                allowedProviders={state.session.allowedProviders}
                chatError={state.chatError}
                chatLoading={state.chatLoading}
                chatResult={state.chatResult}
                cloudModels={state.cloudModels}
                cloudProviders={state.cloudProviders}
                inputClassName={inputClassName}
                localModels={state.localModels}
                localProviders={state.localProviders}
                message={state.message}
                model={state.model}
                providerFilter={state.providerFilter}
                providerScopedModels={state.providerScopedModels}
                runtimeHeaders={state.runtimeHeaders}
                tenantLocked={state.session.kind === "tenant" && state.session.tenant !== ""}
                tenant={state.tenant}
                onMessageChange={actions.setMessage}
                onModelChange={actions.setModel}
                onProviderFilterChange={actions.setProviderFilter}
                onSubmit={actions.submitChat}
                onTenantChange={actions.setTenant}
              />
              <div className="grid gap-4">
                <AuthPanel
                  authToken={state.authToken}
                  inputClassName={inputClassName}
                  sessionAllowedModels={state.session.allowedModels}
                  sessionAllowedProviders={state.session.allowedProviders}
                  sessionCapabilities={state.session.capabilities}
                  sessionKeyID={state.session.keyID}
                  sessionKind={state.session.kind}
                  sessionLabel={state.session.label}
                  sessionName={state.session.name}
                  sessionRole={state.session.role}
                  sessionSource={state.session.source}
                  sessionTenant={state.session.tenant}
                  onAuthTokenChange={actions.setAuthToken}
                  onClearAuthToken={actions.clearAuthToken}
                  onRefresh={actions.loadDashboard}
                />
                <ModelsPanel
                  localModels={state.localModels}
                  modelFilter={state.modelFilter}
                  visibleModels={state.visibleModels}
                  onModelFilterChange={actions.setModelFilter}
                />
              </div>
            </div>
          ) : null}

          {activeView === "providers" ? (
            <div className="grid gap-4 xl:grid-cols-[1.2fr_minmax(0,0.8fr)]">
              <div className="grid gap-4">
                <section className="grid gap-3 md:grid-cols-3">
                  <ProviderHealthStrip
                    label="Cloud runtime"
                    summary={`${state.healthyCloudProviders}/${state.cloudProviders.length || 0} healthy`}
                    tone={state.cloudProviders.length > 0 && state.healthyCloudProviders === state.cloudProviders.length ? "healthy" : "warning"}
                  />
                  <ProviderHealthStrip
                    label="Local runtime"
                    summary={`${state.healthyLocalProviders}/${state.localProviders.length || 0} healthy`}
                    tone={state.localProviders.length > 0 && state.healthyLocalProviders === state.localProviders.length ? "healthy" : "warning"}
                  />
                  <ProviderHealthStrip label="Routing posture" summary={state.runtimeHeaders?.routeReason || "Awaiting request"} tone="neutral" />
                </section>
                <ProvidersPanel copiedCommand={state.copiedCommand} providers={state.providers} onCopyCommand={actions.copyCommand} />
              </div>
              <div className="grid gap-4">
                <LocalRuntimeIssues copiedCommand={state.copiedCommand} issues={state.localProviderIssues} onCopyCommand={actions.copyCommand} />
                <ModelsPanel
                  localModels={state.localModels}
                  modelFilter={state.modelFilter}
                  visibleModels={state.visibleModels}
                  onModelFilterChange={actions.setModelFilter}
                />
              </div>
            </div>
          ) : null}

          {activeView === "budgets" && state.session.isAdmin ? (
            <BudgetPanel
              budget={state.budget}
              budgetActionError={state.budgetActionError}
              budgetAmountUsd={state.budgetAmountUsd}
              budgetLimitUsd={state.budgetLimitUsd}
              inputClassName={inputClassName}
              onBudgetAmountChange={actions.setBudgetAmountUsd}
              onBudgetLimitChange={actions.setBudgetLimitUsd}
              onReset={actions.resetBudget}
              onSetLimit={actions.setBudgetLimit}
              onTopUp={actions.topUpBudget}
            />
          ) : null}

          {activeView === "control-plane" && state.session.isAdmin ? (
            <ControlPlanePanel
              apiKeyFormID={state.apiKeyFormID}
              apiKeyFormModels={state.apiKeyFormModels}
              apiKeyFormName={state.apiKeyFormName}
              apiKeyFormProviders={state.apiKeyFormProviders}
              apiKeyFormRole={state.apiKeyFormRole}
              apiKeyFormSecret={state.apiKeyFormSecret}
              apiKeyFormTenant={state.apiKeyFormTenant}
              controlPlane={state.controlPlane}
              controlPlaneError={state.controlPlaneError}
              inputClassName={inputClassName}
              rotateAPIKeyID={state.rotateAPIKeyID}
              rotateAPIKeySecret={state.rotateAPIKeySecret}
              onDeleteAPIKey={actions.deleteAPIKey}
              onDeleteTenant={actions.deleteTenant}
              tenantFormID={state.tenantFormID}
              tenantFormModels={state.tenantFormModels}
              tenantFormName={state.tenantFormName}
              tenantFormProviders={state.tenantFormProviders}
              onAPIKeyFormIDChange={actions.setAPIKeyFormID}
              onAPIKeyFormModelsChange={actions.setAPIKeyFormModels}
              onAPIKeyFormNameChange={actions.setAPIKeyFormName}
              onAPIKeyFormProvidersChange={actions.setAPIKeyFormProviders}
              onAPIKeyFormRoleChange={actions.setAPIKeyFormRole}
              onAPIKeyFormSecretChange={actions.setAPIKeyFormSecret}
              onAPIKeyFormTenantChange={actions.setAPIKeyFormTenant}
              onRotateAPIKey={actions.rotateAPIKey}
              onSaveAPIKey={actions.upsertAPIKey}
              onSaveTenant={actions.upsertTenant}
              onSetAPIKeyEnabled={actions.setAPIKeyEnabled}
              onSetRotateAPIKeyID={actions.setRotateAPIKeyID}
              onSetRotateAPIKeySecret={actions.setRotateAPIKeySecret}
              onSetTenantEnabled={actions.setTenantEnabled}
              onTenantFormIDChange={actions.setTenantFormID}
              onTenantFormModelsChange={actions.setTenantFormModels}
              onTenantFormNameChange={actions.setTenantFormName}
              onTenantFormProvidersChange={actions.setTenantFormProviders}
            />
          ) : null}

          {activeView === "auth" ? (
            <div className="grid gap-4 xl:grid-cols-[1.05fr_minmax(0,0.95fr)]">
              <AuthPanel
                authToken={state.authToken}
                inputClassName={inputClassName}
                sessionAllowedModels={state.session.allowedModels}
                sessionAllowedProviders={state.session.allowedProviders}
                sessionCapabilities={state.session.capabilities}
                sessionKeyID={state.session.keyID}
                sessionKind={state.session.kind}
                sessionLabel={state.session.label}
                sessionName={state.session.name}
                sessionRole={state.session.role}
                sessionSource={state.session.source}
                sessionTenant={state.session.tenant}
                onAuthTokenChange={actions.setAuthToken}
                onClearAuthToken={actions.clearAuthToken}
                onRefresh={actions.loadDashboard}
              />
              <div className="grid gap-4">
                <ModelsPanel
                  localModels={state.localModels}
                  modelFilter={state.modelFilter}
                  visibleModels={state.visibleModels}
                  onModelFilterChange={actions.setModelFilter}
                />
                {state.session.isAdmin ? (
                  <BudgetPanel
                    budget={state.budget}
                    budgetActionError={state.budgetActionError}
                    budgetAmountUsd={state.budgetAmountUsd}
                    budgetLimitUsd={state.budgetLimitUsd}
                    inputClassName={inputClassName}
                    onBudgetAmountChange={actions.setBudgetAmountUsd}
                    onBudgetLimitChange={actions.setBudgetLimitUsd}
                    onReset={actions.resetBudget}
                    onSetLimit={actions.setBudgetLimit}
                    onTopUp={actions.topUpBudget}
                  />
                ) : null}
              </div>
            </div>
          ) : null}
        </main>
      </div>
    </div>
  );
}
