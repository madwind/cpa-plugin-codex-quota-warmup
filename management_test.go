package main

import (
	"encoding/json"
	"strings"
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

func TestStatusPageIsReadOnlyAndKeyless(t *testing.T) {
	page := renderStatusPage(runtimeSnapshot{
		Version:      "test",
		WorkerActive: true,
		Model:        "gpt-5.6-luna",
		Interval:     "30m",
		Accounts: []accountStatus{{
			Name:     "secret@example.com",
			Email:    "secret@example.com",
			PlanType: "plus",
			Status:   "waiting",
			Windows: []quotaWindowStatus{{
				ID:        "five-hour",
				Label:     "5h",
				Remaining: 100,
			}},
		}},
	})
	for _, forbidden := range []string{"Management Key", "cpa-mgmt-key", "/v0/management", "secret@example.com"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("status page must not contain %q", forbidden)
		}
	}
	for _, want := range []string{"Codex Quota Warmup", "5h", "100.0% remaining", "data-theme"} {
		if !strings.Contains(page, want) {
			t.Fatalf("status page missing %q", want)
		}
	}
}
