//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	dogfoodScorecardSchemaVersion    = "hecate.code-intelligence-dogfood.v2"
	dogfoodProcessCleanupNotMeasured = "not_measured"
)

type dogfoodScorecard struct {
	SchemaVersion   string                  `json:"schema_version"`
	GeneratedAt     string                  `json:"generated_at"`
	SourceRevision  string                  `json:"source_revision"`
	HarnessRevision string                  `json:"harness_revision"`
	Environment     dogfoodEnvironment      `json:"environment"`
	Scenarios       []dogfoodScenarioResult `json:"scenarios"`
	Summary         dogfoodSummary          `json:"summary"`
}

type dogfoodEnvironment struct {
	Version        string                      `json:"version"`
	OS             string                      `json:"os"`
	Arch           string                      `json:"arch"`
	SandboxWrapper string                      `json:"sandbox_wrapper"`
	SourceDirty    bool                        `json:"source_dirty"`
	ModelProvider  string                      `json:"model_provider"`
	Model          string                      `json:"model"`
	Providers      []dogfoodProviderCapability `json:"providers"`
}

type dogfoodProviderCapability struct {
	Language   string   `json:"language"`
	Provider   string   `json:"provider"`
	Version    string   `json:"version,omitempty"`
	Status     string   `json:"status"`
	Available  bool     `json:"available"`
	Operations []string `json:"operations"`
}

type dogfoodScenarioPosture struct {
	ToolsEnabled   bool `json:"tools_enabled"`
	WritesAllowed  bool `json:"writes_allowed"`
	NetworkAllowed bool `json:"network_allowed"`
}

type dogfoodScenarioExpectation struct {
	ID                  string
	Language            string
	Intent              string
	ExpectedRoute       string
	Posture             dogfoodScenarioPosture
	PreferredOperations []string
	Provider            string
	ProviderVersion     string
	ProviderAvailable   bool
	ForcedUnavailable   bool
	PolicyRepresentable bool
	SemanticPermitted   bool
}

type dogfoodRunRef struct {
	TaskID  string `json:"task_id"`
	RunID   string `json:"run_id"`
	TraceID string `json:"trace_id,omitempty"`
}

type dogfoodScenarioObservation struct {
	RunRef                         dogfoodRunRef `json:"run_ref"`
	RunStatus                      string        `json:"run_status"`
	ToolRoute                      []string      `json:"tool_route"`
	ToolRouteModelCalls            []int         `json:"tool_route_model_calls"`
	FirstInspectionTool            string        `json:"first_inspection_tool"`
	ModelCalls                     int           `json:"model_calls"`
	CodeIntelligenceCalls          int           `json:"code_intelligence_calls"`
	SemanticCalls                  int           `json:"semantic_calls"`
	StructuralCalls                int           `json:"structural_calls"`
	GrepCalls                      int           `json:"grep_calls"`
	GrepSuccessfulCalls            int           `json:"grep_successful_calls"`
	Provider                       string        `json:"provider,omitempty"`
	ProviderVersion                string        `json:"observed_provider_version,omitempty"`
	ResultCount                    int           `json:"result_count"`
	SemanticResultCount            int           `json:"semantic_result_count"`
	StructuralResultCount          int           `json:"structural_result_count"`
	GrepResultCount                int           `json:"grep_result_count"`
	SemanticProviderFailureCall    int           `json:"semantic_provider_failure_model_call"`
	StructuralProviderFailureCall  int           `json:"structural_provider_failure_model_call"`
	SemanticProviderFailureStep    int           `json:"semantic_provider_failure_step"`
	StructuralProviderFailureStep  int           `json:"structural_provider_failure_step"`
	SemanticCompletedModelCall     int           `json:"semantic_completed_model_call"`
	StructuralCompletedModelCall   int           `json:"structural_completed_model_call"`
	SemanticCompletedStep          int           `json:"semantic_completed_step"`
	StructuralCompletedStep        int           `json:"structural_completed_step"`
	SemanticFirstResultModelCall   int           `json:"semantic_first_result_model_call"`
	StructuralFirstResultModelCall int           `json:"structural_first_result_model_call"`
	GrepResultModelCall            int           `json:"grep_result_model_call"`
	StructuralResultModelCall      int           `json:"structural_result_model_call"`
	QueryLatencyMeasured           bool          `json:"query_latency_measured"`
	QueryLatencyMillis             int64         `json:"query_latency_ms"`
	RunLatencyMillis               int64         `json:"run_latency_ms"`
	CostMicrosUSD                  int64         `json:"cost_micros_usd"`
	ProcessCleanup                 string        `json:"process_cleanup"`
	WorkspaceChangeCount           int           `json:"workspace_change_count"`
	CapabilitiesBeforeQuery        bool          `json:"capabilities_before_query"`
	ProviderVersionObserved        bool          `json:"provider_version_observed"`
	ProviderUnavailableObserved    bool          `json:"provider_unavailable_observed"`
	SemanticPolicyBlocked          bool          `json:"semantic_policy_blocked"`
	Completed                      bool          `json:"completed"`
	UsefulResult                   bool          `json:"useful_result"`
	UnexpectedToolCalls            int           `json:"unexpected_tool_calls"`
	ErrorKinds                     []string      `json:"error_kinds,omitempty"`
	InvalidRequestReasons          []string      `json:"invalid_request_reasons,omitempty"`
}

type dogfoodScenarioChecks struct {
	CapabilitiesBeforeQuery     bool   `json:"capabilities_before_query"`
	CapabilitiesFirst           bool   `json:"capabilities_first"`
	PreferredRouteFirst         bool   `json:"preferred_route_first"`
	FallbackOnlyAfterPreferred  bool   `json:"fallback_only_after_preferred"`
	ProviderObserved            bool   `json:"provider_observed"`
	ProviderMatched             bool   `json:"provider_matched"`
	ProviderVersionObserved     bool   `json:"provider_version_observed"`
	ProviderUnavailableObserved bool   `json:"provider_unavailable_observed"`
	PreferredToolSelected       bool   `json:"preferred_tool_selected"`
	PreferredToolProducedResult bool   `json:"preferred_tool_produced_result"`
	CorrectFallback             bool   `json:"correct_fallback"`
	UsefulResult                bool   `json:"useful_result"`
	Completed                   bool   `json:"completed"`
	NoWorkspaceWrites           bool   `json:"no_workspace_writes"`
	NoUnexpectedTools           bool   `json:"no_unexpected_tools"`
	PolicyBlockObserved         bool   `json:"policy_block_observed"`
	Cleanup                     string `json:"cleanup"`
}

type dogfoodScenarioResult struct {
	ID                          string                     `json:"id"`
	Language                    string                     `json:"language"`
	Intent                      string                     `json:"intent"`
	ExpectedRoute               string                     `json:"expected_route"`
	Posture                     dogfoodScenarioPosture     `json:"posture"`
	PreferredOperations         []string                   `json:"preferred_operations"`
	ExpectedProvider            string                     `json:"expected_provider"`
	ExpectedVersion             string                     `json:"expected_version,omitempty"`
	CapabilityAwarenessMeasured bool                       `json:"capability_awareness_measured"`
	PreferredRouteAdvertised    bool                       `json:"preferred_route_advertised"`
	PreferredRouteAvailable     bool                       `json:"preferred_route_available"`
	FallbackApplicable          bool                       `json:"fallback_applicable"`
	PolicyRepresentable         bool                       `json:"policy_representable"`
	ProviderQueryFailed         bool                       `json:"provider_query_failed"`
	Observed                    dogfoodScenarioObservation `json:"observed"`
	Checks                      dogfoodScenarioChecks      `json:"checks"`
	Verdict                     string                     `json:"verdict"`
	ReasonCodes                 []string                   `json:"reason_codes"`
}

type dogfoodSummary struct {
	ScenarioCount               int     `json:"scenario_count"`
	Pass                        int     `json:"pass"`
	Fail                        int     `json:"fail"`
	Inconclusive                int     `json:"inconclusive"`
	Skipped                     int     `json:"skipped"`
	CapabilityAwarenessRate     float64 `json:"capability_awareness_rate"`
	CapabilityAwarenessMeasured int     `json:"capability_awareness_measured"`
	PreferredToolRate           float64 `json:"preferred_tool_rate"`
	PreferredResultRate         float64 `json:"preferred_result_rate"`
	PreferredToolMeasured       int     `json:"preferred_tool_measured"`
	FallbackCorrectnessRate     float64 `json:"fallback_correctness_rate"`
	FallbackMeasured            int     `json:"fallback_measured"`
	UsefulResultRate            float64 `json:"useful_result_rate"`
	RunCompletionRate           float64 `json:"run_completion_rate"`
	CleanupMeasured             int     `json:"cleanup_measured"`
	CleanupRate                 float64 `json:"cleanup_rate"`
	QueryLatencyMeasured        int     `json:"query_latency_measured"`
	QueryLatencyMedianMS        int64   `json:"query_latency_median_ms"`
	QueryLatencyMaxMS           int64   `json:"query_latency_max_ms"`
}

func buildDogfoodScenarioResult(expected dogfoodScenarioExpectation, observed dogfoodScenarioObservation) dogfoodScenarioResult {
	providerPrerequisiteMissing := !expected.ProviderAvailable && !expected.ForcedUnavailable &&
		(expected.ExpectedRoute == "semantic" || expected.ExpectedRoute == "structural")
	semanticPolicyUnavailable := expected.ExpectedRoute == "semantic" && !expected.SemanticPermitted
	providerFailureModelCall := 0
	switch expected.ExpectedRoute {
	case "semantic":
		providerFailureModelCall = dogfoodProviderFailureBeforeCompletedQuery(
			observed.SemanticProviderFailureCall,
			observed.SemanticProviderFailureStep,
			observed.SemanticCompletedModelCall,
			observed.SemanticCompletedStep,
			observed.SemanticFirstResultModelCall,
		)
	case "structural":
		providerFailureModelCall = dogfoodProviderFailureBeforeCompletedQuery(
			observed.StructuralProviderFailureCall,
			observed.StructuralProviderFailureStep,
			observed.StructuralCompletedModelCall,
			observed.StructuralCompletedStep,
			observed.StructuralFirstResultModelCall,
		)
	}
	providerQueryFailed := expected.ProviderAvailable && providerFailureModelCall > 0
	preferredRouteAdvertised := (expected.ExpectedRoute == "semantic" && expected.ProviderAvailable && expected.SemanticPermitted) ||
		(expected.ExpectedRoute == "structural" && expected.ProviderAvailable)
	preferredRouteAvailable := preferredRouteAdvertised && !providerQueryFailed
	capabilityAwarenessMeasured := dogfoodInspectionQueryAttempted(observed.ToolRoute)
	preferredToolSelected := (expected.ExpectedRoute == "semantic" && observed.SemanticCalls > 0) ||
		(expected.ExpectedRoute == "structural" && observed.StructuralCalls > 0)
	preferredToolProducedResult := (expected.ExpectedRoute == "semantic" && observed.SemanticResultCount > 0) ||
		(expected.ExpectedRoute == "structural" && observed.StructuralResultCount > 0)
	fallbackApplicable := expected.ExpectedRoute == "fallback" ||
		(expected.ExpectedRoute == "policy" && expected.PolicyRepresentable) || providerPrerequisiteMissing || semanticPolicyUnavailable || providerQueryFailed
	requiresPolicyBlock := semanticPolicyUnavailable || (expected.ExpectedRoute == "policy" && expected.PolicyRepresentable)
	checks := dogfoodScenarioChecks{
		CapabilitiesBeforeQuery:     observed.CapabilitiesBeforeQuery,
		CapabilitiesFirst:           observed.FirstInspectionTool == "code_intelligence:capabilities",
		PreferredRouteFirst:         !preferredRouteAdvertised || dogfoodPreferredRouteFirst(expected.ExpectedRoute, observed.ToolRoute),
		FallbackOnlyAfterPreferred:  !preferredRouteAdvertised || dogfoodFallbackOnlyAfterPreferred(expected.ExpectedRoute, observed, providerFailureModelCall),
		ProviderObserved:            !preferredToolProducedResult || observed.Provider != "",
		ProviderMatched:             !preferredToolSelected || observed.Provider == "" || (expected.Provider != "" && observed.Provider == expected.Provider),
		ProviderVersionObserved:     expected.ProviderVersion == "" || observed.ProviderVersionObserved,
		ProviderUnavailableObserved: !expected.ForcedUnavailable || observed.ProviderUnavailableObserved,
		PreferredToolSelected:       preferredToolSelected,
		PreferredToolProducedResult: preferredToolProducedResult,
		UsefulResult:                observed.UsefulResult,
		Completed:                   observed.Completed,
		NoWorkspaceWrites:           observed.WorkspaceChangeCount == 0,
		NoUnexpectedTools:           observed.UnexpectedToolCalls == 0,
		PolicyBlockObserved:         !requiresPolicyBlock || observed.SemanticPolicyBlocked,
		Cleanup:                     observed.ProcessCleanup,
		CorrectFallback:             true,
	}

	switch expected.ExpectedRoute {
	case "semantic":
		if providerQueryFailed {
			checks.CorrectFallback = observed.SemanticCalls > 0 && dogfoodFallbackProducedResultsAfter(observed, providerFailureModelCall)
		} else if providerPrerequisiteMissing || semanticPolicyUnavailable {
			checks.CorrectFallback = observed.SemanticCalls == 0 && dogfoodFallbackProducedResults(observed)
		}
	case "structural":
		if providerQueryFailed {
			checks.CorrectFallback = observed.StructuralCalls > 0 && observed.GrepResultCount > 0 && observed.GrepResultModelCall > providerFailureModelCall
		} else if providerPrerequisiteMissing {
			checks.CorrectFallback = observed.StructuralCalls == 0 && observed.GrepResultCount > 0
		}
	case "fallback", "policy":
		if expected.ExpectedRoute == "policy" && !expected.PolicyRepresentable {
			checks.CorrectFallback = observed.SemanticResultCount > 0 || dogfoodFallbackProducedResults(observed)
		} else {
			checks.CorrectFallback = observed.SemanticCalls == 0 && dogfoodFallbackProducedResults(observed)
		}
	default:
		checks.CorrectFallback = false
	}

	reasons := make([]string, 0)
	if !checks.Completed {
		reasons = append(reasons, "run_not_completed")
	}
	if !checks.UsefulResult {
		reasons = append(reasons, "answer_marker_missing")
	}
	if observed.WorkspaceChangeCount < 0 {
		reasons = append(reasons, "workspace_change_measurement_failed")
	} else if !checks.NoWorkspaceWrites {
		reasons = append(reasons, "workspace_changed")
	}
	if !checks.NoUnexpectedTools {
		reasons = append(reasons, "unexpected_tool_proposed")
	}
	if !capabilityAwarenessMeasured {
		reasons = append(reasons, "inspection_query_not_attempted")
	} else if !checks.CapabilitiesBeforeQuery {
		reasons = append(reasons, "capabilities_not_consumed_before_query")
	}
	if !checks.CapabilitiesFirst && dogfoodRouteContains(observed.ToolRoute, "code_intelligence:capabilities") {
		reasons = append(reasons, "inspection_before_capabilities")
	}
	if !checks.PreferredRouteFirst {
		reasons = append(reasons, "generic_browse_before_preferred_query")
	}
	if !checks.FallbackOnlyAfterPreferred {
		reasons = append(reasons, "fallback_not_conditioned_on_preferred_result")
	}
	if !checks.ProviderVersionObserved {
		reasons = append(reasons, "provider_version_not_observed")
	}
	if !checks.ProviderObserved {
		reasons = append(reasons, "provider_not_observed")
	} else if !checks.ProviderMatched {
		reasons = append(reasons, "provider_mismatch")
	}
	if !checks.ProviderUnavailableObserved {
		reasons = append(reasons, "forced_provider_unavailability_not_observed")
	}
	if !checks.PolicyBlockObserved {
		reasons = append(reasons, "semantic_policy_block_not_observed")
	}
	if preferredRouteAvailable {
		if !checks.PreferredToolSelected {
			reasons = append(reasons, "preferred_code_intelligence_not_used")
		} else if !checks.PreferredToolProducedResult {
			reasons = append(reasons, "preferred_code_intelligence_no_results")
		}
	}
	if fallbackApplicable && !checks.CorrectFallback {
		fallbackReason := dogfoodFallbackFailureReason(expected, observed)
		if fallbackReason != "" {
			reasons = append(reasons, fallbackReason)
		}
	}
	if (expected.ExpectedRoute == "fallback" || (expected.ExpectedRoute == "policy" && expected.PolicyRepresentable)) && observed.SemanticCalls > 0 {
		reasons = append(reasons, "doomed_semantic_call_attempted")
	}
	if expected.ExpectedRoute == "semantic" && (providerPrerequisiteMissing || semanticPolicyUnavailable) && observed.SemanticCalls > 0 {
		reasons = append(reasons, "doomed_semantic_call_attempted")
	}
	if expected.ExpectedRoute == "structural" && providerPrerequisiteMissing && observed.StructuralCalls > 0 {
		reasons = append(reasons, "doomed_structural_call_attempted")
	}

	verdict := "pass"
	if len(reasons) > 0 {
		verdict = "fail"
	}
	if verdict == "pass" && providerPrerequisiteMissing {
		verdict = "inconclusive"
		reasons = append(reasons, "preferred_provider_unavailable")
	}
	if verdict == "pass" && providerQueryFailed {
		verdict = "inconclusive"
		reasons = append(reasons, "preferred_provider_query_failed")
	}
	if verdict == "pass" && semanticPolicyUnavailable {
		verdict = "inconclusive"
		reasons = append(reasons, "semantic_policy_not_representable")
	}
	if verdict == "pass" && expected.ExpectedRoute == "policy" && !expected.PolicyRepresentable {
		verdict = "inconclusive"
		reasons = append(reasons, "policy_block_not_representable")
	}
	reasons = dogfoodUniqueSorted(reasons)
	return dogfoodScenarioResult{
		ID:                          expected.ID,
		Language:                    expected.Language,
		Intent:                      expected.Intent,
		ExpectedRoute:               expected.ExpectedRoute,
		Posture:                     expected.Posture,
		PreferredOperations:         append([]string(nil), expected.PreferredOperations...),
		ExpectedProvider:            expected.Provider,
		ExpectedVersion:             expected.ProviderVersion,
		CapabilityAwarenessMeasured: capabilityAwarenessMeasured,
		PreferredRouteAdvertised:    preferredRouteAdvertised,
		PreferredRouteAvailable:     preferredRouteAvailable,
		FallbackApplicable:          fallbackApplicable,
		PolicyRepresentable:         expected.PolicyRepresentable,
		ProviderQueryFailed:         providerQueryFailed,
		Observed:                    observed,
		Checks:                      checks,
		Verdict:                     verdict,
		ReasonCodes:                 reasons,
	}
}

func dogfoodProviderFailureBeforeCompletedQuery(providerFailureModelCall, providerFailureStep, completedModelCall, completedStep, firstResultModelCall int) int {
	if providerFailureModelCall <= 0 {
		return 0
	}
	// A later result-producing retry proves the advertised provider route
	// recovered. Do not require a generic fallback after the model already
	// obtained the preferred result.
	if firstResultModelCall > 0 {
		return 0
	}
	// Once the provider has completed a query, even with zero items, a later
	// failure is evidence about that later call rather than retroactive evidence
	// that the advertised route was unavailable.
	if dogfoodQueryPositionBefore(completedModelCall, completedStep, providerFailureModelCall, providerFailureStep) {
		return 0
	}
	return providerFailureModelCall
}

func dogfoodInspectionQueryAttempted(routes []string) bool {
	for _, route := range routes {
		if dogfoodSourceInspectionRoute(route) {
			return true
		}
	}
	return false
}

func dogfoodSourceInspectionRoute(route string) bool {
	switch route {
	case "code_intelligence:definition",
		"code_intelligence:references",
		"code_intelligence:hover",
		"code_intelligence:document_symbols",
		"code_intelligence:workspace_symbols",
		"code_intelligence:diagnostics",
		"code_intelligence:structural_search",
		"code_intelligence:unknown",
		"grep",
		"glob",
		"list_dir",
		"read_file",
		"standalone_structural_search":
		return true
	default:
		return false
	}
}

func dogfoodQueryPositionBefore(leftModelCall, leftStep, rightModelCall, rightStep int) bool {
	if leftModelCall <= 0 || rightModelCall <= 0 {
		return false
	}
	if leftModelCall != rightModelCall {
		return leftModelCall < rightModelCall
	}
	return leftStep > 0 && rightStep > 0 && leftStep < rightStep
}

func dogfoodFallbackProducedResults(observed dogfoodScenarioObservation) bool {
	return observed.GrepResultCount > 0 || observed.StructuralResultCount > 0
}

func dogfoodFallbackProducedResultsAfter(observed dogfoodScenarioObservation, modelCall int) bool {
	return (observed.GrepResultCount > 0 && observed.GrepResultModelCall > modelCall) ||
		(observed.StructuralResultCount > 0 && observed.StructuralResultModelCall > modelCall)
}

func dogfoodFallbackFailureReason(expected dogfoodScenarioExpectation, observed dogfoodScenarioObservation) string {
	prefix := "provider_failure"
	switch expected.ExpectedRoute {
	case "fallback":
		prefix = "missing_provider"
	case "policy":
		prefix = "policy"
	case "semantic":
		if !expected.SemanticPermitted {
			prefix = "semantic_policy"
		} else if !expected.ProviderAvailable {
			prefix = "provider_unavailable"
		}
	case "structural":
		if !expected.ProviderAvailable {
			prefix = "provider_unavailable"
		}
	}
	fallbackAttempted := observed.GrepCalls > 0
	if expected.ExpectedRoute != "structural" {
		fallbackAttempted = fallbackAttempted || observed.StructuralCalls > 0
	}
	if !fallbackAttempted {
		return prefix + "_fallback_not_used"
	}
	if !dogfoodFallbackProducedResults(observed) {
		return prefix + "_fallback_no_results"
	}
	return ""
}

func dogfoodPreferredRouteFirst(expectedRoute string, routes []string) bool {
	for _, route := range routes {
		if route == "code_intelligence:capabilities" {
			continue
		}
		if !dogfoodSourceInspectionRoute(route) {
			continue
		}
		return dogfoodRouteMatchesPreferred(expectedRoute, route)
	}
	// A capabilities-only route did not browse generically. Selection is scored
	// separately, so absence of a preferred query must not be mislabeled as an
	// ordering failure.
	return true
}

func dogfoodFallbackOnlyAfterPreferred(expectedRoute string, observed dogfoodScenarioObservation, providerFailureModelCall int) bool {
	if len(observed.ToolRoute) != len(observed.ToolRouteModelCalls) {
		return false
	}
	preferredCall := 0
	preferredResultModelCall := observed.SemanticFirstResultModelCall
	preferredProducedResults := observed.SemanticResultCount > 0
	if expectedRoute == "structural" {
		preferredResultModelCall = observed.StructuralFirstResultModelCall
		preferredProducedResults = observed.StructuralResultCount > 0
	}
	seenCapabilities := false
	for index, route := range observed.ToolRoute {
		modelCall := observed.ToolRouteModelCalls[index]
		if route == "code_intelligence:capabilities" {
			seenCapabilities = true
			continue
		}
		if !seenCapabilities {
			continue
		}
		if dogfoodRouteMatchesPreferred(expectedRoute, route) {
			if preferredCall == 0 || (modelCall > 0 && modelCall < preferredCall) {
				preferredCall = modelCall
			}
			continue
		}
		if !dogfoodRouteIsFallback(expectedRoute, route) {
			continue
		}
		if preferredProducedResults && preferredResultModelCall <= 0 {
			return false
		}
		if preferredResultModelCall > 0 && preferredResultModelCall <= modelCall {
			return false
		}
		threshold := preferredCall
		if providerFailureModelCall > 0 {
			threshold = providerFailureModelCall
		}
		if threshold <= 0 || modelCall <= threshold {
			return false
		}
	}
	return true
}

func dogfoodRouteContains(routes []string, target string) bool {
	for _, route := range routes {
		if route == target {
			return true
		}
	}
	return false
}

func dogfoodRouteMatchesPreferred(expectedRoute, route string) bool {
	switch expectedRoute {
	case "semantic":
		operation := strings.TrimPrefix(route, "code_intelligence:")
		switch operation {
		case "definition", "references", "hover", "document_symbols", "workspace_symbols", "diagnostics":
			return true
		}
	case "structural":
		return route == "code_intelligence:structural_search"
	}
	return false
}

func dogfoodRouteIsFallback(expectedRoute, route string) bool {
	if route == "grep" {
		return true
	}
	return expectedRoute == "semantic" && route == "code_intelligence:structural_search"
}

func finalizeDogfoodScorecard(card dogfoodScorecard) dogfoodScorecard {
	summary := dogfoodSummary{ScenarioCount: len(card.Scenarios)}
	queryLatencies := make([]int64, 0, len(card.Scenarios))
	cleanupPass := 0
	for _, scenario := range card.Scenarios {
		switch scenario.Verdict {
		case "pass":
			summary.Pass++
		case "fail":
			summary.Fail++
		case "inconclusive":
			summary.Inconclusive++
		case "skipped":
			summary.Skipped++
		}
		if scenario.CapabilityAwarenessMeasured {
			summary.CapabilityAwarenessMeasured++
			if scenario.Checks.CapabilitiesBeforeQuery {
				summary.CapabilityAwarenessRate++
			}
		}
		if scenario.PreferredRouteAdvertised {
			summary.PreferredToolMeasured++
			if scenario.Checks.PreferredToolSelected {
				summary.PreferredToolRate++
			}
			if scenario.Checks.PreferredToolProducedResult {
				summary.PreferredResultRate++
			}
		}
		if scenario.Checks.UsefulResult {
			summary.UsefulResultRate++
		}
		if scenario.Checks.Completed {
			summary.RunCompletionRate++
		}
		if scenario.FallbackApplicable {
			summary.FallbackMeasured++
			if scenario.Checks.CorrectFallback {
				summary.FallbackCorrectnessRate++
			}
		}
		if scenario.Checks.Cleanup != dogfoodProcessCleanupNotMeasured && scenario.Checks.Cleanup != "" {
			summary.CleanupMeasured++
			if scenario.Checks.Cleanup == "pass" {
				cleanupPass++
			}
		}
		if scenario.Observed.QueryLatencyMeasured {
			queryLatencies = append(queryLatencies, scenario.Observed.QueryLatencyMillis)
		}
	}
	denominator := float64(len(card.Scenarios))
	if denominator > 0 {
		summary.UsefulResultRate /= denominator
		summary.RunCompletionRate /= denominator
	}
	if summary.CapabilityAwarenessMeasured > 0 {
		summary.CapabilityAwarenessRate /= float64(summary.CapabilityAwarenessMeasured)
	}
	if summary.PreferredToolMeasured > 0 {
		summary.PreferredToolRate /= float64(summary.PreferredToolMeasured)
		summary.PreferredResultRate /= float64(summary.PreferredToolMeasured)
	}
	if summary.FallbackMeasured > 0 {
		summary.FallbackCorrectnessRate /= float64(summary.FallbackMeasured)
	}
	if summary.CleanupMeasured > 0 {
		summary.CleanupRate = float64(cleanupPass) / float64(summary.CleanupMeasured)
	}
	if len(queryLatencies) > 0 {
		summary.QueryLatencyMeasured = len(queryLatencies)
		sort.Slice(queryLatencies, func(i, j int) bool { return queryLatencies[i] < queryLatencies[j] })
		middle := len(queryLatencies) / 2
		if len(queryLatencies)%2 == 0 {
			summary.QueryLatencyMedianMS = (queryLatencies[middle-1] + queryLatencies[middle]) / 2
		} else {
			summary.QueryLatencyMedianMS = queryLatencies[middle]
		}
		summary.QueryLatencyMaxMS = queryLatencies[len(queryLatencies)-1]
	}
	card.Summary = summary
	return card
}

func dogfoodReportDirectory(repositoryRoot, override string, at time.Time) (string, error) {
	if strings.TrimSpace(override) != "" {
		if filepath.IsAbs(override) {
			return filepath.Clean(override), nil
		}
		return filepath.Join(repositoryRoot, filepath.Clean(override)), nil
	}
	base := filepath.Join(repositoryRoot, ".dogfood", "code-intelligence")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("create scorecard report root: %w", err)
	}
	outputDir, err := os.MkdirTemp(base, at.UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return "", fmt.Errorf("create unique scorecard directory: %w", err)
	}
	return outputDir, nil
}

func writeDogfoodScorecard(outputDir string, card dogfoodScorecard) (string, string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return "", "", fmt.Errorf("scorecard output directory is required")
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create scorecard directory: %w", err)
	}
	jsonBytes, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal scorecard JSON: %w", err)
	}
	jsonBytes = append(jsonBytes, '\n')
	markdownBytes := []byte(renderDogfoodScorecardMarkdown(card))
	jsonPath := filepath.Join(outputDir, "scorecard.json")
	markdownPath := filepath.Join(outputDir, "scorecard.md")
	if err := dogfoodAtomicWrite(jsonPath, jsonBytes); err != nil {
		return "", "", err
	}
	if err := dogfoodAtomicWrite(markdownPath, markdownBytes); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func dogfoodAtomicWrite(path string, content []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".scorecard-*.tmp")
	if err != nil {
		return fmt.Errorf("create scorecard temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set scorecard permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write scorecard: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync scorecard: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close scorecard: %w", err)
	}
	info, statErr := os.Lstat(path)
	switch {
	case os.IsNotExist(statErr):
	case statErr != nil:
		return fmt.Errorf("inspect existing scorecard: %w", statErr)
	case !info.Mode().IsRegular():
		return fmt.Errorf("replace existing scorecard: destination is not a regular file")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish scorecard: %w", err)
	}
	return nil
}

func renderDogfoodScorecardMarkdown(card dogfoodScorecard) string {
	verdict := "PASS"
	if card.Summary.Fail > 0 {
		verdict = "FAIL"
	} else if card.Summary.Inconclusive > 0 || card.Summary.Skipped > 0 {
		verdict = "INCONCLUSIVE"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Code Intelligence Dogfood Scorecard — %s\n\n", dogfoodMarkdownCell(card.GeneratedAt))
	fmt.Fprintf(&builder, "Overall verdict: **%s**\n\n", verdict)
	builder.WriteString("## Provenance\n\n")
	builder.WriteString("| Field | Value |\n| --- | --- |\n")
	fmt.Fprintf(&builder, "| Schema | %s |\n", dogfoodMarkdownCell(card.SchemaVersion))
	fmt.Fprintf(&builder, "| Source revision | `%s` |\n", dogfoodMarkdownCell(card.SourceRevision))
	fmt.Fprintf(&builder, "| Harness revision | %s |\n", dogfoodMarkdownCell(card.HarnessRevision))
	fmt.Fprintf(&builder, "| Hecate version | %s |\n", dogfoodMarkdownCell(card.Environment.Version))
	fmt.Fprintf(&builder, "| Platform | %s/%s |\n", dogfoodMarkdownCell(card.Environment.OS), dogfoodMarkdownCell(card.Environment.Arch))
	fmt.Fprintf(&builder, "| Sandbox wrapper | %s |\n", dogfoodMarkdownCell(card.Environment.SandboxWrapper))
	fmt.Fprintf(&builder, "| Model route | %s / %s |\n", dogfoodMarkdownCell(card.Environment.ModelProvider), dogfoodMarkdownCell(card.Environment.Model))
	fmt.Fprintf(&builder, "| Source dirty | %t |\n\n", card.Environment.SourceDirty)

	builder.WriteString("## Provider baseline\n\n")
	builder.WriteString("| Language | Provider | Version | Status | Available |\n| --- | --- | --- | --- | --- |\n")
	for _, provider := range card.Environment.Providers {
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %t |\n",
			dogfoodMarkdownCell(provider.Language), dogfoodMarkdownCell(provider.Provider), dogfoodMarkdownCell(provider.Version), dogfoodMarkdownCell(provider.Status), provider.Available)
	}

	builder.WriteString("\n## Aggregate metrics\n\n")
	builder.WriteString("| Metric | Value |\n| --- | ---: |\n")
	fmt.Fprintf(&builder, "| Pass / fail / inconclusive / skipped | %d / %d / %d / %d |\n", card.Summary.Pass, card.Summary.Fail, card.Summary.Inconclusive, card.Summary.Skipped)
	if card.Summary.CapabilityAwarenessMeasured == 0 {
		builder.WriteString("| Capability awareness | not measured (0 measured) |\n")
	} else {
		fmt.Fprintf(&builder, "| Capability awareness | %.0f%% (%d measured) |\n", card.Summary.CapabilityAwarenessRate*100, card.Summary.CapabilityAwarenessMeasured)
	}
	if card.Summary.PreferredToolMeasured == 0 {
		builder.WriteString("| Preferred tool selection | not measured (0 measured) |\n")
		builder.WriteString("| Preferred tool produced results | not measured (0 measured) |\n")
	} else {
		fmt.Fprintf(&builder, "| Preferred tool selection | %.0f%% (%d measured) |\n", card.Summary.PreferredToolRate*100, card.Summary.PreferredToolMeasured)
		fmt.Fprintf(&builder, "| Preferred tool produced results | %.0f%% (%d measured) |\n", card.Summary.PreferredResultRate*100, card.Summary.PreferredToolMeasured)
	}
	if card.Summary.FallbackMeasured == 0 {
		builder.WriteString("| Qualifying fallback success | not measured (0 measured) |\n")
	} else {
		fmt.Fprintf(&builder, "| Qualifying fallback success | %.0f%% (%d measured) |\n", card.Summary.FallbackCorrectnessRate*100, card.Summary.FallbackMeasured)
	}
	fmt.Fprintf(&builder, "| Useful result | %.0f%% |\n", card.Summary.UsefulResultRate*100)
	fmt.Fprintf(&builder, "| Run completion | %.0f%% |\n", card.Summary.RunCompletionRate*100)
	if card.Summary.QueryLatencyMeasured == 0 {
		builder.WriteString("| Query latency median / max | not measured (0 measured) |\n")
	} else {
		fmt.Fprintf(&builder, "| Query latency median / max | %d / %d ms (%d measured) |\n", card.Summary.QueryLatencyMedianMS, card.Summary.QueryLatencyMaxMS, card.Summary.QueryLatencyMeasured)
	}
	if card.Summary.CleanupMeasured == 0 {
		builder.WriteString("| Process cleanup | not measured |\n")
	} else {
		fmt.Fprintf(&builder, "| Process cleanup | %.0f%% (%d measured) |\n", card.Summary.CleanupRate*100, card.Summary.CleanupMeasured)
	}

	builder.WriteString("\n## Scenarios\n\n")
	builder.WriteString("| Scenario | Language | Posture | Expected | Observed route | Useful | Run complete | Query ms | Cleanup | Verdict |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- |\n")
	for _, scenario := range card.Scenarios {
		posture := fmt.Sprintf("tools=%t writes=%t network=%t", scenario.Posture.ToolsEnabled, scenario.Posture.WritesAllowed, scenario.Posture.NetworkAllowed)
		queryLatency := "not measured"
		if scenario.Observed.QueryLatencyMeasured {
			queryLatency = strconv.FormatInt(scenario.Observed.QueryLatencyMillis, 10)
		}
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %s | %t | %t | %s | %s | %s |\n",
			dogfoodMarkdownCell(scenario.ID), dogfoodMarkdownCell(scenario.Language), dogfoodMarkdownCell(posture), dogfoodMarkdownCell(scenario.ExpectedRoute),
			dogfoodMarkdownCell(strings.Join(scenario.Observed.ToolRoute, " → ")), scenario.Checks.UsefulResult, scenario.Checks.Completed,
			dogfoodMarkdownCell(queryLatency), dogfoodMarkdownCell(scenario.Checks.Cleanup), dogfoodMarkdownCell(strings.ToUpper(scenario.Verdict)))
	}

	builder.WriteString("\n## Findings and limitations\n\n")
	for _, scenario := range card.Scenarios {
		if len(scenario.ReasonCodes) > 0 {
			fmt.Fprintf(&builder, "- `%s`: %s.\n", dogfoodMarkdownCell(scenario.ID), dogfoodMarkdownCell(strings.Join(scenario.ReasonCodes, ", ")))
		}
		if len(scenario.Observed.InvalidRequestReasons) > 0 {
			fmt.Fprintf(&builder, "- `%s` invalid request reasons: %s.\n", dogfoodMarkdownCell(scenario.ID), dogfoodMarkdownCell(strings.Join(scenario.Observed.InvalidRequestReasons, ", ")))
		}
	}
	builder.WriteString("- Process cleanup is intentionally not inferred from host process listings; deterministic provider-supervision tests own that assertion.\n")
	builder.WriteString("- The scorecard stores no task prompt, final model answer, source result text, query, path, raw provider error, or process argv.\n")

	builder.WriteString("\n## Runtime references\n\n")
	builder.WriteString("| Scenario | Task | Run | Trace |\n| --- | --- | --- | --- |\n")
	for _, scenario := range card.Scenarios {
		fmt.Fprintf(&builder, "| %s | `%s` | `%s` | `%s` |\n", dogfoodMarkdownCell(scenario.ID), dogfoodMarkdownCell(scenario.Observed.RunRef.TaskID), dogfoodMarkdownCell(scenario.Observed.RunRef.RunID), dogfoodMarkdownCell(scenario.Observed.RunRef.TraceID))
	}
	return builder.String()
}

func dogfoodMarkdownCell(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("|", "\\|", "\r", " ", "\n", " ", "`", "\\`").Replace(value)
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512])
	}
	return value
}

func dogfoodUniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func TestDogfoodScorecardScoring(t *testing.T) {
	baseExpected := dogfoodScenarioExpectation{
		ID: "go-semantic-r1", Language: "go", Intent: "semantic_symbol_lookup", ExpectedRoute: "semantic",
		Posture:  dogfoodScenarioPosture{ToolsEnabled: true, WritesAllowed: true, NetworkAllowed: false},
		Provider: "gopls", ProviderVersion: "0.20.0", ProviderAvailable: true, PolicyRepresentable: true, SemanticPermitted: true,
	}
	baseObserved := dogfoodScenarioObservation{
		RunRef: dogfoodRunRef{TaskID: "task_1", RunID: "run_1"}, RunStatus: "completed",
		ToolRoute: []string{"code_intelligence:capabilities", "code_intelligence:document_symbols"}, ToolRouteModelCalls: []int{1, 2}, FirstInspectionTool: "code_intelligence:capabilities",
		ModelCalls: 3, CodeIntelligenceCalls: 2, SemanticCalls: 1, Provider: "gopls", ProviderVersion: "0.20.0", ResultCount: 1, SemanticResultCount: 1, SemanticCompletedModelCall: 2, SemanticCompletedStep: 2, SemanticFirstResultModelCall: 2,
		QueryLatencyMeasured: true, QueryLatencyMillis: 120, RunLatencyMillis: 800, ProcessCleanup: dogfoodProcessCleanupNotMeasured, WorkspaceChangeCount: 0,
		CapabilitiesBeforeQuery: true, ProviderVersionObserved: true, Completed: true, UsefulResult: true,
	}
	if result := buildDogfoodScenarioResult(baseExpected, baseObserved); result.Verdict != "pass" {
		t.Fatalf("semantic verdict = %q reasons=%v, want pass", result.Verdict, result.ReasonCodes)
	}
	queryFirstObserved := baseObserved
	queryFirstObserved.ToolRoute = []string{"code_intelligence:workspace_symbols"}
	queryFirstObserved.ToolRouteModelCalls = []int{1}
	queryFirstObserved.FirstInspectionTool = "code_intelligence:workspace_symbols"
	queryFirstObserved.CodeIntelligenceCalls = 1
	queryFirstObserved.SemanticCompletedModelCall = 1
	queryFirstObserved.SemanticCompletedStep = 1
	queryFirstObserved.CapabilitiesBeforeQuery = false
	queryFirstObserved.ProviderVersion = ""
	queryFirstObserved.ProviderVersionObserved = false
	queryFirstResult := buildDogfoodScenarioResult(baseExpected, queryFirstObserved)
	if queryFirstResult.Verdict != "fail" || !queryFirstResult.Checks.PreferredRouteFirst || !queryFirstResult.Checks.PreferredToolSelected || !queryFirstResult.Checks.PreferredToolProducedResult || !queryFirstResult.Checks.ProviderMatched {
		t.Fatalf("query-first result = verdict=%q checks=%+v reasons=%v, want successful preferred route with strict capability-adherence failure", queryFirstResult.Verdict, queryFirstResult.Checks, queryFirstResult.ReasonCodes)
	}
	for _, misleading := range []string{"generic_browse_before_preferred_query", "inspection_before_capabilities", "provider_mismatch", "provider_not_observed", "preferred_code_intelligence_not_used"} {
		if dogfoodContains(queryFirstResult.ReasonCodes, misleading) {
			t.Fatalf("query-first reasons=%v, must not contain %q", queryFirstResult.ReasonCodes, misleading)
		}
	}
	if !dogfoodContains(queryFirstResult.ReasonCodes, "capabilities_not_consumed_before_query") || !dogfoodContains(queryFirstResult.ReasonCodes, "provider_version_not_observed") {
		t.Fatalf("query-first reasons=%v, want explicit capability-adherence reasons", queryFirstResult.ReasonCodes)
	}

	invalidRequestObserved := baseObserved
	invalidRequestObserved.Provider = ""
	invalidRequestObserved.ResultCount = 0
	invalidRequestObserved.SemanticResultCount = 0
	invalidRequestObserved.SemanticFirstResultModelCall = 0
	invalidRequestObserved.SemanticCompletedModelCall = 0
	invalidRequestObserved.SemanticCompletedStep = 0
	invalidRequestObserved.ErrorKinds = []string{"invalid_request"}
	invalidRequestObserved.InvalidRequestReasons = []string{"path_required"}
	invalidRequestResult := buildDogfoodScenarioResult(baseExpected, invalidRequestObserved)
	if invalidRequestResult.Verdict != "fail" || !invalidRequestResult.Checks.PreferredToolSelected || invalidRequestResult.Checks.PreferredToolProducedResult || !dogfoodContains(invalidRequestResult.ReasonCodes, "preferred_code_intelligence_no_results") {
		t.Fatalf("invalid-request result = verdict=%q checks=%+v reasons=%v, want selected query without results", invalidRequestResult.Verdict, invalidRequestResult.Checks, invalidRequestResult.ReasonCodes)
	}
	if dogfoodContains(invalidRequestResult.ReasonCodes, "provider_mismatch") || dogfoodContains(invalidRequestResult.ReasonCodes, "provider_not_observed") {
		t.Fatalf("invalid-request reasons=%v, must not infer provider evidence without a result", invalidRequestResult.ReasonCodes)
	}
	if got := renderDogfoodScorecardMarkdown(finalizeDogfoodScorecard(dogfoodScorecard{Scenarios: []dogfoodScenarioResult{invalidRequestResult}})); !strings.Contains(got, "invalid request reasons: path_required") {
		t.Fatalf("invalid-request markdown omitted the closed diagnostic: %s", got)
	}
	wrongZeroResultProviderObserved := invalidRequestObserved
	wrongZeroResultProviderObserved.Provider = "tsc"
	if result := buildDogfoodScenarioResult(baseExpected, wrongZeroResultProviderObserved); !dogfoodContains(result.ReasonCodes, "provider_mismatch") {
		t.Fatalf("wrong zero-result provider reasons=%v, want observed mismatch", result.ReasonCodes)
	}
	missingResultProviderObserved := baseObserved
	missingResultProviderObserved.Provider = ""
	missingResultProviderResult := buildDogfoodScenarioResult(baseExpected, missingResultProviderObserved)
	if !dogfoodContains(missingResultProviderResult.ReasonCodes, "provider_not_observed") || dogfoodContains(missingResultProviderResult.ReasonCodes, "provider_mismatch") {
		t.Fatalf("missing-provider-summary reasons=%v, want provider_not_observed only", missingResultProviderResult.ReasonCodes)
	}

	wrongProviderObserved := baseObserved
	wrongProviderObserved.Provider = "tsc"
	if result := buildDogfoodScenarioResult(baseExpected, wrongProviderObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "provider_mismatch") {
		t.Fatalf("wrong-provider verdict = %q reasons=%v, want provider mismatch", result.Verdict, result.ReasonCodes)
	}
	genericFirstObserved := baseObserved
	genericFirstObserved.ToolRoute = []string{"code_intelligence:capabilities", "grep", "code_intelligence:document_symbols"}
	genericFirstObserved.ToolRouteModelCalls = []int{1, 2, 3}
	if result := buildDogfoodScenarioResult(baseExpected, genericFirstObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "generic_browse_before_preferred_query") {
		t.Fatalf("generic-first verdict = %q reasons=%v, want preferred-route ordering failure", result.Verdict, result.ReasonCodes)
	}
	effectfulBeforePreferredObserved := baseObserved
	effectfulBeforePreferredObserved.ToolRoute = []string{"code_intelligence:capabilities", "effectful_builtin", "code_intelligence:document_symbols"}
	effectfulBeforePreferredObserved.ToolRouteModelCalls = []int{1, 2, 3}
	effectfulBeforePreferredObserved.UnexpectedToolCalls = 1
	if result := buildDogfoodScenarioResult(baseExpected, effectfulBeforePreferredObserved); !result.Checks.PreferredRouteFirst || dogfoodContains(result.ReasonCodes, "generic_browse_before_preferred_query") {
		t.Fatalf("effectful-before-preferred checks=%+v reasons=%v, want unrelated proposal excluded from source-inspection ordering", result.Checks, result.ReasonCodes)
	}
	capabilitiesOnlyObserved := baseObserved
	capabilitiesOnlyObserved.ToolRoute = []string{"code_intelligence:capabilities"}
	capabilitiesOnlyObserved.ToolRouteModelCalls = []int{1}
	capabilitiesOnlyObserved.CodeIntelligenceCalls = 1
	capabilitiesOnlyObserved.SemanticCalls = 0
	capabilitiesOnlyObserved.ResultCount = 0
	capabilitiesOnlyObserved.SemanticResultCount = 0
	capabilitiesOnlyObserved.SemanticFirstResultModelCall = 0
	capabilitiesOnlyObserved.SemanticCompletedModelCall = 0
	capabilitiesOnlyObserved.SemanticCompletedStep = 0
	capabilitiesOnlyObserved.Provider = ""
	capabilitiesOnlyResult := buildDogfoodScenarioResult(baseExpected, capabilitiesOnlyObserved)
	if capabilitiesOnlyResult.CapabilityAwarenessMeasured || capabilitiesOnlyResult.Checks.PreferredToolSelected || !capabilitiesOnlyResult.Checks.PreferredRouteFirst {
		t.Fatalf("capabilities-only checks=%+v, want missing selection without an ordering failure", capabilitiesOnlyResult.Checks)
	}
	if dogfoodContains(capabilitiesOnlyResult.ReasonCodes, "capabilities_not_consumed_before_query") ||
		dogfoodContains(capabilitiesOnlyResult.ReasonCodes, "generic_browse_before_preferred_query") ||
		!dogfoodContains(capabilitiesOnlyResult.ReasonCodes, "inspection_query_not_attempted") ||
		!dogfoodContains(capabilitiesOnlyResult.ReasonCodes, "preferred_code_intelligence_not_used") {
		t.Fatalf("capabilities-only reasons=%v, want an unmeasured missing inspection without capability-consumption or generic-browse labels", capabilitiesOnlyResult.ReasonCodes)
	}
	capabilityCard := finalizeDogfoodScorecard(dogfoodScorecard{Scenarios: []dogfoodScenarioResult{
		buildDogfoodScenarioResult(baseExpected, baseObserved),
		capabilitiesOnlyResult,
	}})
	if capabilityCard.Summary.CapabilityAwarenessMeasured != 1 || capabilityCard.Summary.CapabilityAwarenessRate != 1 {
		t.Fatalf("capability-awareness summary = %+v, want one measured successful inspection", capabilityCard.Summary)
	}
	if markdown := renderDogfoodScorecardMarkdown(capabilityCard); !strings.Contains(markdown, "| Capability awareness | 100% (1 measured) |") {
		t.Fatalf("capability-awareness markdown omitted denominator: %s", markdown)
	}
	unmeasuredCapabilityCard := finalizeDogfoodScorecard(dogfoodScorecard{Scenarios: []dogfoodScenarioResult{capabilitiesOnlyResult}})
	if markdown := renderDogfoodScorecardMarkdown(unmeasuredCapabilityCard); !strings.Contains(markdown, "| Capability awareness | not measured (0 measured) |") || strings.Contains(markdown, "| Capability awareness | 0%") {
		t.Fatalf("unmeasured capability-awareness markdown was ambiguous: %s", markdown)
	}
	zeroMeasuredMarkdown := renderDogfoodScorecardMarkdown(finalizeDogfoodScorecard(dogfoodScorecard{}))
	for _, metric := range []string{"Capability awareness", "Preferred tool selection", "Preferred tool produced results", "Qualifying fallback success"} {
		if !strings.Contains(zeroMeasuredMarkdown, "| "+metric+" | not measured (0 measured) |") {
			t.Fatalf("zero-denominator %s metric was ambiguous: %s", metric, zeroMeasuredMarkdown)
		}
	}
	if !strings.Contains(zeroMeasuredMarkdown, "| Query latency median / max | not measured (0 measured) |") {
		t.Fatalf("zero-denominator query latency metric was ambiguous: %s", zeroMeasuredMarkdown)
	}
	latencyCard := finalizeDogfoodScorecard(dogfoodScorecard{Scenarios: []dogfoodScenarioResult{
		{Observed: dogfoodScenarioObservation{QueryLatencyMeasured: true, QueryLatencyMillis: 0}},
		{Observed: dogfoodScenarioObservation{QueryLatencyMeasured: true, QueryLatencyMillis: 10}},
		{Observed: dogfoodScenarioObservation{QueryLatencyMeasured: false, QueryLatencyMillis: 999}},
	}})
	if latencyCard.Summary.QueryLatencyMeasured != 2 || latencyCard.Summary.QueryLatencyMedianMS != 5 || latencyCard.Summary.QueryLatencyMaxMS != 10 {
		t.Fatalf("measured query latency summary = count %d median %d max %d, want 2 measurements and 5/10 with zero included and unmeasured excluded", latencyCard.Summary.QueryLatencyMeasured, latencyCard.Summary.QueryLatencyMedianMS, latencyCard.Summary.QueryLatencyMaxMS)
	}
	effectfulOnlyObserved := capabilitiesOnlyObserved
	effectfulOnlyObserved.ToolRoute = []string{"code_intelligence:capabilities", "effectful_builtin"}
	effectfulOnlyObserved.ToolRouteModelCalls = []int{1, 2}
	effectfulOnlyObserved.CapabilitiesBeforeQuery = false
	effectfulOnlyObserved.UnexpectedToolCalls = 1
	effectfulOnlyResult := buildDogfoodScenarioResult(baseExpected, effectfulOnlyObserved)
	if effectfulOnlyResult.CapabilityAwarenessMeasured || dogfoodContains(effectfulOnlyResult.ReasonCodes, "capabilities_not_consumed_before_query") || !dogfoodContains(effectfulOnlyResult.ReasonCodes, "inspection_query_not_attempted") {
		t.Fatalf("effectful-only capability scoring = measured=%t reasons=%v, want no source-inspection measurement", effectfulOnlyResult.CapabilityAwarenessMeasured, effectfulOnlyResult.ReasonCodes)
	}
	readAfterPreferredObserved := baseObserved
	readAfterPreferredObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:document_symbols", "read_file"}
	readAfterPreferredObserved.ToolRouteModelCalls = []int{1, 2, 3}
	if result := buildDogfoodScenarioResult(baseExpected, readAfterPreferredObserved); result.Verdict != "pass" || !result.Checks.FallbackOnlyAfterPreferred {
		t.Fatalf("read-after-preferred verdict = %q checks=%+v reasons=%v, want ordered targeted read", result.Verdict, result.Checks, result.ReasonCodes)
	}
	fallbackBeforeLaterResultObserved := baseObserved
	fallbackBeforeLaterResultObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:document_symbols", "grep", "code_intelligence:document_symbols"}
	fallbackBeforeLaterResultObserved.ToolRouteModelCalls = []int{1, 2, 3, 4}
	fallbackBeforeLaterResultObserved.SemanticCalls = 2
	fallbackBeforeLaterResultObserved.SemanticFirstResultModelCall = 4
	fallbackBeforeLaterResultObserved.GrepCalls = 1
	fallbackBeforeLaterResultObserved.GrepSuccessfulCalls = 1
	fallbackBeforeLaterResultObserved.GrepResultCount = 1
	fallbackBeforeLaterResultObserved.GrepResultModelCall = 3
	if result := buildDogfoodScenarioResult(baseExpected, fallbackBeforeLaterResultObserved); result.Verdict != "pass" || !result.Checks.FallbackOnlyAfterPreferred {
		t.Fatalf("fallback-before-later-result verdict = %q checks=%+v reasons=%v, want fallback judged from evidence available at call 3", result.Verdict, result.Checks, result.ReasonCodes)
	}
	unneededFallbackObserved := baseObserved
	unneededFallbackObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:document_symbols", "grep"}
	unneededFallbackObserved.ToolRouteModelCalls = []int{1, 2, 2}
	unneededFallbackObserved.GrepCalls = 1
	unneededFallbackObserved.GrepSuccessfulCalls = 1
	unneededFallbackObserved.GrepResultCount = 1
	unneededFallbackObserved.GrepResultModelCall = 2
	if result := buildDogfoodScenarioResult(baseExpected, unneededFallbackObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "fallback_not_conditioned_on_preferred_result") {
		t.Fatalf("unneeded fallback verdict = %q reasons=%v, want fallback-discipline failure", result.Verdict, result.ReasonCodes)
	}
	recoveredProviderObserved := baseObserved
	recoveredProviderObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:document_symbols", "code_intelligence:document_symbols"}
	recoveredProviderObserved.ToolRouteModelCalls = []int{1, 2, 3}
	recoveredProviderObserved.SemanticCalls = 2
	recoveredProviderObserved.SemanticProviderFailureCall = 2
	recoveredProviderObserved.SemanticProviderFailureStep = 2
	recoveredProviderObserved.SemanticCompletedModelCall = 3
	recoveredProviderObserved.SemanticCompletedStep = 3
	recoveredProviderObserved.SemanticFirstResultModelCall = 3
	if result := buildDogfoodScenarioResult(baseExpected, recoveredProviderObserved); result.Verdict != "pass" || result.ProviderQueryFailed || result.FallbackApplicable || !result.Checks.PreferredToolProducedResult {
		t.Fatalf("recovered-provider verdict = %q provider_failed=%t fallback=%t checks=%+v reasons=%v, want successful preferred retry", result.Verdict, result.ProviderQueryFailed, result.FallbackApplicable, result.Checks, result.ReasonCodes)
	}
	providerFailureObserved := baseObserved
	providerFailureObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:document_symbols", "grep"}
	providerFailureObserved.ToolRouteModelCalls = []int{1, 2, 3}
	providerFailureObserved.SemanticResultCount = 0
	providerFailureObserved.SemanticFirstResultModelCall = 0
	providerFailureObserved.SemanticCompletedModelCall = 0
	providerFailureObserved.SemanticCompletedStep = 0
	providerFailureObserved.ResultCount = 0
	providerFailureObserved.Provider = ""
	providerFailureObserved.GrepCalls = 1
	providerFailureObserved.GrepSuccessfulCalls = 1
	providerFailureObserved.GrepResultCount = 1
	providerFailureObserved.SemanticProviderFailureCall = 2
	providerFailureObserved.SemanticProviderFailureStep = 2
	providerFailureObserved.GrepResultModelCall = 3
	providerFailureObserved.ErrorKinds = []string{"provider_protocol"}
	providerFailureResult := buildDogfoodScenarioResult(baseExpected, providerFailureObserved)
	if providerFailureResult.Verdict != "inconclusive" || !providerFailureResult.Checks.CorrectFallback || !dogfoodContains(providerFailureResult.ReasonCodes, "preferred_provider_query_failed") {
		t.Fatalf("query-failure verdict = %q checks=%+v reasons=%v, want inconclusive fallback", providerFailureResult.Verdict, providerFailureResult.Checks, providerFailureResult.ReasonCodes)
	}
	if !providerFailureResult.PreferredRouteAdvertised || providerFailureResult.PreferredRouteAvailable {
		t.Fatalf("query-failure route posture = advertised=%t available=%t, want advertised baseline with runtime failure", providerFailureResult.PreferredRouteAdvertised, providerFailureResult.PreferredRouteAvailable)
	}
	queryFailureCard := finalizeDogfoodScorecard(dogfoodScorecard{Scenarios: []dogfoodScenarioResult{providerFailureResult}})
	if queryFailureCard.Summary.PreferredToolMeasured != 1 || queryFailureCard.Summary.PreferredToolRate != 1 || queryFailureCard.Summary.PreferredResultRate != 0 {
		t.Fatalf("query-failure preferred summary = %+v, want selected query included in advertised-route denominator", queryFailureCard.Summary)
	}
	firstFailureThenCompletionObserved := providerFailureObserved
	firstFailureThenCompletionObserved.SemanticCompletedModelCall = 4
	firstFailureThenCompletionObserved.SemanticCompletedStep = 4
	if result := buildDogfoodScenarioResult(baseExpected, firstFailureThenCompletionObserved); !result.ProviderQueryFailed || result.Verdict != "inconclusive" {
		t.Fatalf("first-failure-then-completion result = provider_query_failed=%t verdict=%q reasons=%v, want first failure to retain fallback semantics", result.ProviderQueryFailed, result.Verdict, result.ReasonCodes)
	}
	providerFailureObserved.GrepResultModelCall = 2
	providerFailureObserved.ToolRouteModelCalls = []int{1, 2, 2}
	if result := buildDogfoodScenarioResult(baseExpected, providerFailureObserved); result.Verdict != "fail" || result.Checks.CorrectFallback {
		t.Fatalf("parallel fallback verdict = %q checks=%+v reasons=%v, want temporal-order failure", result.Verdict, result.Checks, result.ReasonCodes)
	}
	providerFailureObserved.GrepResultModelCall = 3
	providerFailureObserved.ToolRouteModelCalls = []int{1, 2, 3}
	unrelatedFailureObserved := providerFailureObserved
	unrelatedFailureObserved.SemanticProviderFailureCall = 0
	unrelatedFailureObserved.StructuralProviderFailureCall = 2
	if result := buildDogfoodScenarioResult(baseExpected, unrelatedFailureObserved); result.ProviderQueryFailed || result.Verdict != "fail" {
		t.Fatalf("unrelated failure verdict = %q provider_query_failed=%t reasons=%v, want ordinary semantic failure", result.Verdict, result.ProviderQueryFailed, result.ReasonCodes)
	}
	providerFailureObserved.GrepCalls = 0
	providerFailureObserved.GrepSuccessfulCalls = 0
	providerFailureObserved.GrepResultCount = 0
	providerFailureObserved.GrepResultModelCall = 0
	providerFailureObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:document_symbols"}
	providerFailureObserved.ToolRouteModelCalls = []int{1, 2}
	if result := buildDogfoodScenarioResult(baseExpected, providerFailureObserved); result.Verdict != "fail" {
		t.Fatalf("query failure without fallback verdict = %q reasons=%v, want fail", result.Verdict, result.ReasonCodes)
	}
	structuralFailureExpected := baseExpected
	structuralFailureExpected.ID = "python-structural-provider-failure-r1"
	structuralFailureExpected.Language = "python"
	structuralFailureExpected.ExpectedRoute = "structural"
	structuralFailureExpected.Provider = "ast-grep"
	structuralFailureObserved := providerFailureObserved
	structuralFailureObserved.SemanticCalls = 0
	structuralFailureObserved.SemanticProviderFailureCall = 0
	structuralFailureObserved.StructuralCalls = 1
	structuralFailureObserved.StructuralCompletedModelCall = 0
	structuralFailureObserved.StructuralCompletedStep = 0
	structuralFailureObserved.StructuralProviderFailureCall = 2
	structuralFailureObserved.StructuralProviderFailureStep = 2
	structuralFailureObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:structural_search"}
	structuralFailureResult := buildDogfoodScenarioResult(structuralFailureExpected, structuralFailureObserved)
	if !dogfoodContains(structuralFailureResult.ReasonCodes, "provider_failure_fallback_not_used") || dogfoodContains(structuralFailureResult.ReasonCodes, "provider_failure_fallback_no_results") {
		t.Fatalf("structural provider failure reasons=%v, want grep fallback not used", structuralFailureResult.ReasonCodes)
	}

	fallbackExpected := baseExpected
	fallbackExpected.ID = "missing-go-provider-r1"
	fallbackExpected.ExpectedRoute = "fallback"
	fallbackExpected.ProviderAvailable = false
	fallbackExpected.ForcedUnavailable = true
	fallbackExpected.ProviderVersion = ""
	fallbackObserved := baseObserved
	fallbackObserved.ToolRoute = []string{"code_intelligence:capabilities", "grep"}
	fallbackObserved.SemanticCalls = 0
	fallbackObserved.SemanticResultCount = 0
	fallbackObserved.SemanticFirstResultModelCall = 0
	fallbackObserved.GrepCalls = 1
	fallbackObserved.GrepSuccessfulCalls = 1
	fallbackObserved.GrepResultCount = 1
	fallbackObserved.Provider = ""
	fallbackObserved.ProviderVersion = ""
	fallbackObserved.ProviderVersionObserved = false
	fallbackObserved.ProviderUnavailableObserved = true
	if result := buildDogfoodScenarioResult(fallbackExpected, fallbackObserved); result.Verdict != "pass" {
		t.Fatalf("fallback verdict = %q reasons=%v, want pass", result.Verdict, result.ReasonCodes)
	}
	fallbackNoResultsObserved := fallbackObserved
	fallbackNoResultsObserved.GrepResultCount = 0
	if result := buildDogfoodScenarioResult(fallbackExpected, fallbackNoResultsObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "missing_provider_fallback_no_results") || dogfoodContains(result.ReasonCodes, "missing_provider_fallback_not_used") {
		t.Fatalf("fallback-no-results verdict = %q reasons=%v, want attempted fallback without results", result.Verdict, result.ReasonCodes)
	}
	invalidFallbackObserved := fallbackObserved
	invalidFallbackObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:unknown", "grep"}
	invalidFallbackObserved.UnexpectedToolCalls = 1
	if result := buildDogfoodScenarioResult(fallbackExpected, invalidFallbackObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "unexpected_tool_proposed") {
		t.Fatalf("invalid-operation fallback verdict = %q reasons=%v, want unexpected-tool failure", result.Verdict, result.ReasonCodes)
	}

	naturalMissingExpected := baseExpected
	naturalMissingExpected.ID = "go-provider-prerequisite-missing-r1"
	naturalMissingExpected.ProviderAvailable = false
	naturalMissingExpected.ProviderVersion = ""
	naturalMissingObserved := fallbackObserved
	naturalMissingObserved.ProviderUnavailableObserved = false
	if result := buildDogfoodScenarioResult(naturalMissingExpected, naturalMissingObserved); result.Verdict != "inconclusive" {
		t.Fatalf("natural missing-provider verdict = %q reasons=%v, want inconclusive", result.Verdict, result.ReasonCodes)
	}
	naturalMissingObserved.GrepCalls = 0
	naturalMissingObserved.GrepSuccessfulCalls = 0
	naturalMissingObserved.GrepResultCount = 0
	naturalMissingObserved.ToolRoute = []string{"code_intelligence:capabilities"}
	naturalMissingResult := buildDogfoodScenarioResult(naturalMissingExpected, naturalMissingObserved)
	if naturalMissingResult.Verdict != "fail" {
		t.Fatalf("missing fallback verdict = %q reasons=%v, want fail", naturalMissingResult.Verdict, naturalMissingResult.ReasonCodes)
	}
	naturalMissingCard := finalizeDogfoodScorecard(dogfoodScorecard{Scenarios: []dogfoodScenarioResult{naturalMissingResult}})
	if naturalMissingCard.Summary.FallbackMeasured != 1 || naturalMissingCard.Summary.FallbackCorrectnessRate != 0 {
		t.Fatalf("missing fallback summary = %+v, want one measured incorrect fallback", naturalMissingCard.Summary)
	}

	structuralMissingExpected := naturalMissingExpected
	structuralMissingExpected.ID = "structural-provider-prerequisite-missing-r1"
	structuralMissingExpected.Language = "python"
	structuralMissingExpected.ExpectedRoute = "structural"
	structuralMissingExpected.Provider = "ast-grep"
	structuralDoomedObserved := fallbackObserved
	structuralDoomedObserved.ToolRoute = []string{"code_intelligence:capabilities", "code_intelligence:structural_search", "grep"}
	structuralDoomedObserved.StructuralCalls = 1
	structuralDoomedObserved.StructuralResultCount = 0
	structuralDoomedObserved.StructuralFirstResultModelCall = 0
	if result := buildDogfoodScenarioResult(structuralMissingExpected, structuralDoomedObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "doomed_structural_call_attempted") {
		t.Fatalf("doomed structural verdict = %q reasons=%v, want unavailable-provider call failure", result.Verdict, result.ReasonCodes)
	}

	unsafeObserved := baseObserved
	unsafeObserved.WorkspaceChangeCount = 1
	if result := buildDogfoodScenarioResult(baseExpected, unsafeObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "workspace_changed") {
		t.Fatalf("unsafe verdict = %q reasons=%v, want workspace failure", result.Verdict, result.ReasonCodes)
	}

	unexpectedObserved := baseObserved
	unexpectedObserved.UnexpectedToolCalls = 1
	if result := buildDogfoodScenarioResult(baseExpected, unexpectedObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "unexpected_tool_proposed") {
		t.Fatalf("unexpected-tool verdict = %q reasons=%v, want safety failure", result.Verdict, result.ReasonCodes)
	}

	hostBlockedExpected := baseExpected
	hostBlockedExpected.SemanticPermitted = false
	hostBlockedObserved := baseObserved
	hostBlockedObserved.ToolRoute = []string{"code_intelligence:capabilities", "grep"}
	hostBlockedObserved.SemanticCalls = 0
	hostBlockedObserved.SemanticResultCount = 0
	hostBlockedObserved.SemanticFirstResultModelCall = 0
	hostBlockedObserved.GrepCalls = 1
	hostBlockedObserved.GrepSuccessfulCalls = 1
	hostBlockedObserved.GrepResultCount = 1
	hostBlockedObserved.SemanticPolicyBlocked = true
	if result := buildDogfoodScenarioResult(hostBlockedExpected, hostBlockedObserved); result.Verdict != "inconclusive" || !dogfoodContains(result.ReasonCodes, "semantic_policy_not_representable") {
		t.Fatalf("host-policy verdict = %q reasons=%v, want inconclusive", result.Verdict, result.ReasonCodes)
	}

	restrictedExpected := baseExpected
	restrictedExpected.ID = "restricted-go-r1"
	restrictedExpected.ExpectedRoute = "policy"
	restrictedExpected.Posture.WritesAllowed = false
	restrictedObserved := baseObserved
	restrictedObserved.ToolRoute = []string{"code_intelligence:capabilities", "grep"}
	restrictedObserved.SemanticCalls = 0
	restrictedObserved.SemanticResultCount = 0
	restrictedObserved.SemanticFirstResultModelCall = 0
	restrictedObserved.GrepCalls = 1
	restrictedObserved.GrepSuccessfulCalls = 1
	restrictedObserved.GrepResultCount = 1
	restrictedObserved.SemanticPolicyBlocked = true
	if result := buildDogfoodScenarioResult(restrictedExpected, restrictedObserved); result.Verdict != "pass" {
		t.Fatalf("restricted-policy verdict = %q reasons=%v, want pass", result.Verdict, result.ReasonCodes)
	}
	restrictedObserved.SemanticPolicyBlocked = false
	if result := buildDogfoodScenarioResult(restrictedExpected, restrictedObserved); result.Verdict != "fail" || !dogfoodContains(result.ReasonCodes, "semantic_policy_block_not_observed") {
		t.Fatalf("unobserved policy verdict = %q reasons=%v, want policy evidence failure", result.Verdict, result.ReasonCodes)
	}

	selectionCard := finalizeDogfoodScorecard(dogfoodScorecard{Scenarios: []dogfoodScenarioResult{invalidRequestResult}})
	if selectionCard.Summary.PreferredToolRate != 1 || selectionCard.Summary.PreferredResultRate != 0 {
		t.Fatalf("selection/result summary = %+v, want selected=100%% produced=0%%", selectionCard.Summary)
	}
}

func TestDogfoodScorecardWriteProducesBoundedArtifacts(t *testing.T) {
	card := finalizeDogfoodScorecard(dogfoodScorecard{
		SchemaVersion:   dogfoodScorecardSchemaVersion,
		GeneratedAt:     "2026-08-08T10:00:00Z",
		SourceRevision:  strings.Repeat("a", 40),
		HarnessRevision: "project-agent-loop-v1",
		Environment:     dogfoodEnvironment{Version: "dev", OS: "test", Arch: "test", SandboxWrapper: "none"},
		Scenarios: []dogfoodScenarioResult{{
			ID: "safe-r1", Language: "go", Intent: "semantic_symbol_lookup", ExpectedRoute: "semantic",
			Posture:  dogfoodScenarioPosture{ToolsEnabled: true},
			Observed: dogfoodScenarioObservation{RunRef: dogfoodRunRef{TaskID: "task_safe", RunID: "run_safe"}, ToolRoute: []string{"code_intelligence:capabilities"}, ProcessCleanup: dogfoodProcessCleanupNotMeasured},
			Verdict:  "inconclusive", ReasonCodes: []string{"provider_unavailable"},
		}},
	})
	outputDir := filepath.Join(t.TempDir(), "scorecard")
	jsonPath, markdownPath, err := writeDogfoodScorecard(outputDir, card)
	if err != nil {
		t.Fatalf("write scorecard: %v", err)
	}
	if _, _, err := writeDogfoodScorecard(outputDir, card); err != nil {
		t.Fatalf("replace scorecard in fixed output directory: %v", err)
	}
	if info, err := os.Stat(outputDir); err != nil {
		t.Fatalf("stat scorecard directory: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("scorecard directory permissions = %o, want 700", info.Mode().Perm())
	}
	for _, path := range []string{jsonPath, markdownPath} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read scorecard %s: %v", filepath.Base(path), err)
		}
		if len(content) == 0 {
			t.Fatalf("scorecard %s is empty", filepath.Base(path))
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat scorecard %s: %v", filepath.Base(path), err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("scorecard %s permissions = %o, want 600", filepath.Base(path), info.Mode().Perm())
		}
	}
	jsonContent, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read scorecard JSON schema: %v", err)
	}
	if !strings.Contains(string(jsonContent), `"schema_version": "hecate.code-intelligence-dogfood.v2"`) ||
		!strings.Contains(string(jsonContent), `"run_completion_rate"`) ||
		strings.Contains(string(jsonContent), `"task_completion_rate"`) {
		t.Fatalf("scorecard JSON did not use the v2 Run-completion schema: %s", jsonContent)
	}
	markdownContent, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read scorecard Markdown schema: %v", err)
	}
	if !strings.Contains(string(markdownContent), "| Run completion |") || strings.Contains(string(markdownContent), "| Task completion |") ||
		!strings.Contains(string(markdownContent), "| Qualifying fallback success |") || strings.Contains(string(markdownContent), "| Structured fallback success |") {
		t.Fatalf("scorecard Markdown did not use v2 metric terminology: %s", markdownContent)
	}
}

func TestDogfoodReportDirectoryDefaultIsUnique(t *testing.T) {
	repositoryRoot := t.TempDir()
	at := time.Date(2026, time.August, 8, 10, 0, 0, 0, time.UTC)
	first, err := dogfoodReportDirectory(repositoryRoot, "", at)
	if err != nil {
		t.Fatalf("create first default scorecard directory: %v", err)
	}
	second, err := dogfoodReportDirectory(repositoryRoot, "", at)
	if err != nil {
		t.Fatalf("create second default scorecard directory: %v", err)
	}
	if first == second {
		t.Fatal("default scorecard directories collided for the same timestamp")
	}
	base := filepath.Join(repositoryRoot, ".dogfood", "code-intelligence") + string(os.PathSeparator)
	for _, directory := range []string{first, second} {
		if !strings.HasPrefix(directory, base+"20260808T100000Z-") {
			t.Fatalf("default scorecard directory %q is outside the expected unique report root", directory)
		}
		if info, statErr := os.Stat(directory); statErr != nil || !info.IsDir() {
			t.Fatalf("default scorecard directory was not created atomically: info=%v err=%v", info, statErr)
		}
	}
	override, err := dogfoodReportDirectory(repositoryRoot, "custom-report", at)
	if err != nil {
		t.Fatalf("resolve scorecard override: %v", err)
	}
	if override != filepath.Join(repositoryRoot, "custom-report") {
		t.Fatalf("scorecard override = %q, want repository-relative path", override)
	}
}

func TestDogfoodSourceInspectionRoutePredicateIsClosed(t *testing.T) {
	for _, route := range []string{
		"code_intelligence:definition",
		"code_intelligence:unknown",
		"grep",
		"glob",
		"list_dir",
		"read_file",
		"standalone_structural_search",
	} {
		if !dogfoodSourceInspectionRoute(route) {
			t.Errorf("source-inspection route %q was not measured", route)
		}
	}
	for _, route := range []string{
		"code_intelligence:capabilities",
		"code_intelligence:invented",
		"effectful_builtin",
		"unknown_tool",
		"artifact_read",
		"git_status",
		"git_diff",
	} {
		if dogfoodSourceInspectionRoute(route) {
			t.Errorf("non-source route %q was measured", route)
		}
	}
}

func dogfoodContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
