package browserrunner

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidateFlowRequestCanonicalizesBoundedExactFlow(t *testing.T) {
	t.Parallel()
	request, err := ValidateFlowRequest(FlowRequest{
		URL:           " HTTPS://App.Example.test:443/reports ",
		AllowedOrigin: "https://app.example.test/",
		Actions: []FlowAction{
			{Kind: " click ", Role: " button ", Name: " Continue "},
			{Kind: "wait_for", Role: "status", Name: "Ready"},
		},
	})
	if err != nil {
		t.Fatalf("ValidateFlowRequest() error = %v", err)
	}
	want := FlowRequest{
		URL:           "https://App.Example.test:443/reports",
		AllowedOrigin: "https://app.example.test",
		Actions: []FlowAction{
			{Kind: FlowActionClick, Role: "button", Name: "Continue"},
			{Kind: FlowActionWaitFor, Role: "status", Name: "Ready"},
		},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("ValidateFlowRequest() = %#v, want %#v", request, want)
	}
}

func TestValidateFlowRequestRejectsUnsafeOrUnboundedFlows(t *testing.T) {
	t.Parallel()
	valid := FlowRequest{
		URL:           "https://app.example.test/",
		AllowedOrigin: "https://app.example.test",
		Actions:       []FlowAction{{Kind: FlowActionClick, Role: "button", Name: "Continue"}},
	}
	tests := map[string]FlowRequest{
		"query":              withFlowURL(valid, "https://app.example.test/?token=secret"),
		"origin mismatch":    withFlowOrigin(valid, "https://other.example.test"),
		"no actions":         withFlowActions(valid, nil),
		"too many actions":   withFlowActions(valid, repeatFlowAction(valid.Actions[0], MaxFlowActions+1)),
		"unknown kind":       withFlowActions(valid, []FlowAction{{Kind: "type", Role: "textbox", Name: "Secret"}}),
		"non-clickable role": withFlowActions(valid, []FlowAction{{Kind: FlowActionClick, Role: "heading", Name: "Summary"}}),
		"empty name":         withFlowActions(valid, []FlowAction{{Kind: FlowActionWaitFor, Role: "status"}}),
		"control":            withFlowActions(valid, []FlowAction{{Kind: FlowActionWaitFor, Role: "status", Name: "Ready\nNow"}}),
		"bidi control":       withFlowActions(valid, []FlowAction{{Kind: FlowActionWaitFor, Role: "status", Name: "Ready\u202e"}}),
		"oversized role":     withFlowActions(valid, []FlowAction{{Kind: FlowActionWaitFor, Role: strings.Repeat("r", 65), Name: "Ready"}}),
		"oversized name":     withFlowActions(valid, []FlowAction{{Kind: FlowActionWaitFor, Role: "status", Name: strings.Repeat("n", 257)}}),
	}
	for name, request := range tests {
		request := request
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ValidateFlowRequest(request); !errors.Is(err, ErrInvalidFlow) {
				t.Fatalf("ValidateFlowRequest() error = %v, want ErrInvalidFlow", err)
			}
		})
	}
}

func withFlowURL(request FlowRequest, value string) FlowRequest {
	request.URL = value
	return request
}

func withFlowOrigin(request FlowRequest, value string) FlowRequest {
	request.AllowedOrigin = value
	return request
}

func withFlowActions(request FlowRequest, value []FlowAction) FlowRequest {
	request.Actions = value
	return request
}

func repeatFlowAction(action FlowAction, count int) []FlowAction {
	out := make([]FlowAction, count)
	for index := range out {
		out[index] = action
	}
	return out
}
