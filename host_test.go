package main

import "testing"

func TestParseAuthMaterial(t *testing.T) {
	raw := []byte(`{"access_token":"token-a","account_id":"acct-a"}`)
	material, err := parseAuthMaterial(raw)
	if err != nil {
		t.Fatal(err)
	}
	if material.AccessToken != "token-a" || material.AccountID != "acct-a" {
		t.Fatalf("unexpected material: %#v", material)
	}
}

func TestParseAuthMaterialNested(t *testing.T) {
	raw := []byte(`{"tokens":{"access_token":"token-b","chatgpt_account_id":"acct-b"}}`)
	material, err := parseAuthMaterial(raw)
	if err != nil {
		t.Fatal(err)
	}
	if material.AccessToken != "token-b" || material.AccountID != "acct-b" {
		t.Fatalf("unexpected material: %#v", material)
	}
}
