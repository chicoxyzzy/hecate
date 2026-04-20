import { BudgetPanel } from "../components/BudgetPanel";
import { ControlPlanePanel } from "../components/ControlPlanePanel";
import { LocalRuntimeIssues } from "../components/LocalRuntimeIssues";
import { ModelsPanel } from "../components/ModelsPanel";
import { PlaygroundPanel } from "../components/PlaygroundPanel";
import { ProviderHealthStrip } from "../components/ProviderHealthStrip";
import { ProvidersPanel } from "../components/ProvidersPanel";
import { StatCard } from "../components/StatCard";
import { useRuntimeConsole } from "./useRuntimeConsole";

const inputClassName =
  "w-full rounded-2xl border border-slate-200/80 bg-white/90 px-4 py-3 text-slate-900 outline-none transition focus:border-cyan-700 focus:ring-4 focus:ring-cyan-100";

export default function App() {
  const { state, actions } = useRuntimeConsole();

  return (
    <div className="min-h-screen bg-[radial-gradient(circle_at_top_left,rgba(255,198,120,0.35),transparent_28%),radial-gradient(circle_at_top_right,rgba(72,164,255,0.24),transparent_26%),linear-gradient(180deg,#f9f2e8_0%,#eef5f8_100%)] text-slate-900">
      <div className="mx-auto max-w-7xl px-5 py-8 md:px-6 md:py-10">
        <header className="mb-6 flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <p className="mb-1 text-xs font-semibold uppercase tracking-[0.22em] text-amber-700">AI Agent Runtime</p>
            <h1 className="font-serif text-5xl leading-none md:text-7xl">Hecate Console</h1>
            <p className="mt-3 max-w-3xl text-base text-slate-600 md:text-lg">
              A thin operator console for probing models, providers, cache behavior, and budget state.
            </p>
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

        <section className="mb-4 grid gap-3 md:grid-cols-3">
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

        <LocalRuntimeIssues copiedCommand={state.copiedCommand} issues={state.localProviderIssues} onCopyCommand={actions.copyCommand} />

        <section className="mb-4 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <StatCard label="Gateway Status" value={state.health?.status ?? (state.loading ? "loading" : "unknown")} />
          <StatCard label="Providers Healthy" value={`${state.healthyProviders}/${state.providers.length || 0}`} />
          <StatCard label="Models Discovered" value={`${state.models.length} total`} />
          <StatCard label="Budget Scope" value={state.budget?.scope ?? "global"} />
        </section>

        <section className="mb-4 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <StatCard label="Cloud Providers" value={String(state.cloudProviders.length)} />
          <StatCard label="Local Providers" value={String(state.localProviders.length)} />
          <StatCard label="Cloud Models" value={String(state.cloudModels.length)} />
          <StatCard label="Local Models" value={String(state.localModels.length)} />
        </section>

        <main className="grid gap-4 xl:grid-cols-[1.7fr_minmax(0,1fr)]">
          <PlaygroundPanel
            authToken={state.authToken}
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
            tenant={state.tenant}
            onAuthTokenChange={actions.setAuthToken}
            onMessageChange={actions.setMessage}
            onModelChange={actions.setModel}
            onProviderFilterChange={actions.setProviderFilter}
            onSubmit={actions.submitChat}
            onTenantChange={actions.setTenant}
          />

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

          <ProvidersPanel copiedCommand={state.copiedCommand} providers={state.providers} onCopyCommand={actions.copyCommand} />

          <ModelsPanel localModels={state.localModels} modelFilter={state.modelFilter} visibleModels={state.visibleModels} onModelFilterChange={actions.setModelFilter} />
        </main>
      </div>
    </div>
  );
}
