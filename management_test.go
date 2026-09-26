package main

import (
	"strings"
	"encoding/json"
	"testing"
)

func TestManagementRegistrationSerializable(t *testing.T) {
	raw, err := json.Marshal(managementRegistration())
	if err != nil {
		t.Fatalf("management registration must be JSON-serializable: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("management registration JSON is empty")
	}
}

func TestStatusPageReusesCPAStoredCredentials(t *testing.T) {
	page := renderStatusPage()
	for _, want := range []string{
		"cli-proxy-auth",
		"cli-proxy-api-webui::secure-storage",
		"readSavedCPAKey",
		"sessionStorage",
		"data-theme",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("status page missing %q", want)
		}
	}
}
