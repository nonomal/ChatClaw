package openclawruntime

import "testing"

func TestGatewayOperatorScopes_IncludeAdminForQueryClient(t *testing.T) {
	mainScopes := gatewayOperatorScopes()
	queryScopes := gatewayQueryOperatorScopes()

	if len(mainScopes) == 0 {
		t.Fatalf("expected main scopes to be configured")
	}
	if len(queryScopes) == 0 {
		t.Fatalf("expected query scopes to be configured")
	}
	if !containsString(mainScopes, "operator.admin") {
		t.Fatalf("expected main scopes to include operator.admin, got %v", mainScopes)
	}
	if !containsString(queryScopes, "operator.admin") {
		t.Fatalf("expected query scopes to include operator.admin, got %v", queryScopes)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
