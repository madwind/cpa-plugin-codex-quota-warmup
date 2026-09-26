package main

import (
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
