package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDestinationMatching(t *testing.T) {
	for _, tc := range []struct {
		kind, value, destination string
		match, known             bool
	}{
		{"DOMAIN-SUFFIX", "example.com", "www.example.com", true, true},
		{"DOMAIN-SUFFIX", "example.com", "badexample.com", false, true},
		{"DOMAIN", "example.com", "www.example.com", false, true},
		{"IP-CIDR", "192.0.2.0/24", "192.0.2.1", true, true},
		{"IP-CIDR6", "2001:db8::/32", "2001:db8::1", true, true},
		{"IP-CIDR", "192.0.2.0/24", "example.com", false, false},
		{"GEOIP", "CN", "1.1.1.1", false, false},
	} {
		m, k := destinationMatch(tc.kind, tc.value, tc.destination)
		if m != tc.match || k != tc.known {
			t.Errorf("%+v: %v %v", tc, m, k)
		}
	}
	for _, s := range []string{"https://example.com", "example.com/path", "x,y", "-invalid.com", ""} {
		if _, err := normalizeDestination(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestLookupOverridePrecedenceAndUpdate(t *testing.T) {
	a := testApp(t)
	for _, target := range []string{"REJECT", "DIRECT"} {
		w := httptest.NewRecorder()
		a.handleRuleOverride(w, httptest.NewRequest("POST", "/api/rules/override", strings.NewReader(`{"destination":"Example.COM.","target":"`+target+`"}`)))
		if w.Code != 201 {
			t.Fatal(w.Body.String())
		}
		w = httptest.NewRecorder()
		a.handleRuleLookup(w, httptest.NewRequest("GET", "/api/rules/lookup?destination=example.com", nil))
		var result struct {
			Target  string
			Certain bool
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Target != target || !result.Certain {
			t.Fatal(w.Body.String())
		}
		ordered := orderedRoutingRules(a.listRules(), a.listServiceRuleGroups())
		if ordered[0].value != "DOMAIN,example.com,"+target {
			t.Fatal(ordered[0])
		}
	}
	var count int
	a.db.QueryRow(`SELECT COUNT(*) FROM rules WHERE rule_type='DOMAIN' AND match_value='example.com'`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}

func TestProviderLookup(t *testing.T) {
	a := testApp(t)
	_, err := a.db.Exec(`INSERT INTO rule_providers(name,behavior,provider_format,primary_url,content,enabled,created_at) VALUES('lookup-test','domain','yaml','https://example.com/rules',?,1,'now')`, "payload:\n  - '+.example.com'\n")
	if err != nil {
		t.Fatal(err)
	}
	if m, k := a.providerDestinationMatch("lookup-test", "www.example.com"); !m || !k {
		t.Fatal(m, k)
	}
	if m, k := a.providerDestinationMatch("missing", "example.com"); m || k {
		t.Fatal(m, k)
	}
}

func TestLookupDownloadsAndChecksEveryReferencedProvider(t *testing.T) {
	a := testApp(t)
	if _, err := a.db.Exec(`DELETE FROM rules; DELETE FROM service_rule_groups; DELETE FROM rule_providers`); err != nil {
		t.Fatal(err)
	}
	requests := map[string]int{}
	a.ruleProviderClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests[r.URL.Path]++
		body := ""
		status := http.StatusOK
		switch r.URL.Path {
		case "/first":
			body = "payload:\n  - '+.other.example'\n"
		case "/second":
			body = "payload:\n  - DOMAIN,unrelated.example\n  - DOMAIN-SUFFIX,example.com\n"
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	for index, provider := range []struct{ name, path, behavior string }{
		{name: "first-provider", path: "/first", behavior: "domain"},
		{name: "second-provider", path: "/second", behavior: "classical"},
	} {
		if _, err := a.db.Exec(`INSERT INTO rule_providers(name,behavior,provider_format,primary_url,content,enabled,created_at) VALUES(?,?,'yaml',?,'',1,'now')`, provider.name, provider.behavior, "https://rules.example"+provider.path); err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec(`INSERT INTO rules(rule_type,match_value,target,priority,enabled,created_at) VALUES('RULE-SET',?,?,?,1,'now')`, provider.name, []string{"DIRECT", "REJECT"}[index], index+1); err != nil {
			t.Fatal(err)
		}
	}

	lookup := func() struct {
		Target       string `json:"target"`
		Rule         string `json:"rule"`
		ProviderRule string `json:"providerRule"`
		Certain      bool   `json:"certain"`
	} {
		w := httptest.NewRecorder()
		a.handleRuleLookup(w, httptest.NewRequest("GET", "/api/rules/lookup?destination=www.example.com", nil))
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		var result struct {
			Target       string `json:"target"`
			Rule         string `json:"rule"`
			ProviderRule string `json:"providerRule"`
			Certain      bool   `json:"certain"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	result := lookup()
	if !result.Certain || result.Target != "REJECT" || result.Rule != "RULE-SET,second-provider,REJECT" || result.ProviderRule != "DOMAIN-SUFFIX,example.com" {
		t.Fatalf("unexpected provider lookup: %+v", result)
	}
	if requests["/first"] != 1 || requests["/second"] != 1 {
		t.Fatalf("did not download every provider required by rule order: %+v", requests)
	}
	result = lookup()
	if !result.Certain || requests["/first"] != 1 || requests["/second"] != 1 {
		t.Fatalf("cached lookup fetched providers again: result=%+v requests=%+v", result, requests)
	}
}

func TestProviderLookupSupportsRulesKeyAndDomainMatchers(t *testing.T) {
	a := testApp(t)
	content := "rules:\n  - DOMAIN-WILDCARD,api?.example.com\n  - DOMAIN-REGEX,^cdn[0-9]+\\.example\\.net$\n"
	if _, err := a.db.Exec(`INSERT INTO rule_providers(name,behavior,provider_format,primary_url,content,enabled,created_at) VALUES('matcher-test','classical','yaml','https://example.com/rules',?,1,'now')`, content); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"api1.example.com", "cdn42.example.net"} {
		matched, known, providerRule := a.providerDestinationMatchDetail("MATCHER-TEST", destination)
		if !matched || !known || providerRule == "" {
			t.Fatalf("%s: matched=%v known=%v rule=%q", destination, matched, known, providerRule)
		}
	}
}

func TestProviderDomainWildcards(t *testing.T) {
	for _, tc := range []struct {
		pattern, destination string
		matched              bool
	}{
		{"+.Example.COM", "example.com", true}, {"+.example.com", "a.b.example.com", true},
		{".example.com", "example.com", false}, {".EXAMPLE.com", "a.b.example.com", true},
		{"*.example.com", "a.example.com", true}, {"*.example.com", "a.b.example.com", false},
		{"*.*.example.com", "a.b.example.com", true}, {"*.*.example.com", "a.example.com", false},
		{"+.example.com", "badexample.com", false}, {"+.1", "192.0.2.1", false},
	} {
		matched, known := providerDomainMatch(tc.pattern, tc.destination)
		if !known || matched != tc.matched {
			t.Errorf("%+v: %v %v", tc, matched, known)
		}
	}
}

func TestLookupUnknownEarlierRules(t *testing.T) {
	a := testApp(t)
	if _, err := a.db.Exec(`DELETE FROM rules; DELETE FROM service_rule_groups`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO rules(rule_type,match_value,target,priority,enabled,created_at) VALUES('GEOIP','CN','DIRECT',1,1,'now'),('DOMAIN-SUFFIX','example.com','REJECT',2,1,'now')`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.handleRuleLookup(w, httptest.NewRequest("GET", "/api/rules/lookup?destination=example.com", nil))
	var result struct {
		Certain    bool
		Target     string
		Unresolved []string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Certain || result.Target != "REJECT" || len(result.Unresolved) != 1 {
		t.Fatal(w.Body.String())
	}
	if _, err := a.db.Exec(`UPDATE rules SET enabled=0 WHERE rule_type='GEOIP'`); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	a.handleRuleLookup(w, httptest.NewRequest("GET", "/api/rules/lookup?destination=example.com", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Certain || len(result.Unresolved) != 0 {
		t.Fatal(w.Body.String())
	}
}

func TestOverrideSurvivesReorderRenameEditAndMigration(t *testing.T) {
	a := testApp(t)
	inserted, err := a.db.Exec(`INSERT INTO proxy_groups(name,group_type,enabled,created_at) VALUES('Lookup group','select',1,'now')`)
	if err != nil {
		t.Fatal(err)
	}
	groupID, _ := inserted.LastInsertId()
	if _, err := a.db.Exec(`INSERT INTO service_rule_groups(name,rules_json,target_group_id,priority,enabled,created_at) VALUES('lookup service','["DOMAIN-SUFFIX,example.com"]',?,-100,1,'now')`, groupID); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.handleRuleOverride(w, httptest.NewRequest("POST", "/api/rules/override", strings.NewReader(fmt.Sprintf(`{"destination":"example.com","target":"group-id:%d"}`, groupID))))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	rules := a.listRules()
	override := rules[0]
	if !override.IsOverride || override.TargetGroupID != groupID {
		t.Fatal(override)
	}
	ids := []int64{}
	for i := len(rules) - 1; i >= 0; i-- {
		ids = append(ids, rules[i].ID)
	}
	payload, _ := json.Marshal(map[string]any{"ids": ids})
	w = httptest.NewRecorder()
	a.handleRuleReorder(w, httptest.NewRequest("PATCH", "/api/rules/reorder", strings.NewReader(string(payload))))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := a.db.Exec(`UPDATE proxy_groups SET name='Renamed lookup' WHERE id=?`, groupID); err != nil {
		t.Fatal(err)
	}
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	if first := orderedRoutingRules(a.listRules(), a.listServiceRuleGroups())[0]; first.value != "DOMAIN,example.com,Renamed lookup" {
		t.Fatal(first)
	}
	// The normal editor must retain the override tier when changing its destination.
	override.Match = "other.example.com"
	override.Priority = 999
	override.Target = "Renamed lookup"
	payload, _ = json.Marshal(override)
	w = httptest.NewRecorder()
	a.handleRuleAction(w, httptest.NewRequest("PATCH", fmt.Sprintf("/api/rules/%d", override.ID), strings.NewReader(string(payload))))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if first := orderedRoutingRules(a.listRules(), a.listServiceRuleGroups())[0]; first.value != "DOMAIN,other.example.com,Renamed lookup" {
		t.Fatal(first)
	}
	w = httptest.NewRecorder()
	a.handleRuleAction(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/rules/%d", override.ID), nil))
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	for _, rule := range a.listRules() {
		if rule.ID == override.ID {
			t.Fatal("override was not deleted")
		}
	}
}

func TestOverrideInvalidTargetsAndIP(t *testing.T) {
	a := testApp(t)
	for _, target := range []string{"", "missing", "group-id:999999", "DIRECT,REJECT"} {
		payload, _ := json.Marshal(map[string]string{"destination": "example.com", "target": target})
		w := httptest.NewRecorder()
		a.handleRuleOverride(w, httptest.NewRequest("POST", "/api/rules/override", strings.NewReader(string(payload))))
		if w.Code != 400 {
			t.Fatalf("%q: %d %s", target, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct{ destination, rule string }{{"192.0.2.1", "IP-CIDR,192.0.2.1/32,DIRECT"}, {"2001:db8::1", "IP-CIDR6,2001:db8::1/128,DIRECT"}} {
		w := httptest.NewRecorder()
		a.handleRuleOverride(w, httptest.NewRequest("POST", "/api/rules/override", strings.NewReader(fmt.Sprintf(`{"destination":%q,"target":"DIRECT"}`, tc.destination))))
		if w.Code != 201 {
			t.Fatal(w.Body.String())
		}
		if !strings.Contains(a.generateConfig(), "  - "+tc.rule+"\n") {
			t.Fatal("missing exact IP override")
		}
	}
	for _, value := range []string{"999.999.999.999", "fe80::1%en0", "1234"} {
		if _, err := normalizeDestination(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestOverrideSupportsDomainMatchTypes(t *testing.T) {
	a := testApp(t)
	for _, tc := range []struct {
		ruleType string
		want     string
	}{
		{ruleType: "DOMAIN", want: "DOMAIN,piwheels.org,DIRECT"},
		{ruleType: "DOMAIN-SUFFIX", want: "DOMAIN-SUFFIX,piwheels.org,DIRECT"},
		{ruleType: "DOMAIN-KEYWORD", want: "DOMAIN-KEYWORD,piwheels.org,DIRECT"},
	} {
		body := fmt.Sprintf(`{"destination":"Piwheels.ORG.","ruleType":%q,"target":"DIRECT"}`, tc.ruleType)
		w := httptest.NewRecorder()
		a.handleRuleOverride(w, httptest.NewRequest(http.MethodPost, "/api/rules/override", strings.NewReader(body)))
		if w.Code != http.StatusCreated {
			t.Fatalf("%s returned %d: %s", tc.ruleType, w.Code, w.Body.String())
		}
		if config := a.generateConfig(); !strings.Contains(config, "  - "+tc.want+"\n") {
			t.Fatalf("generated config is missing %q", tc.want)
		}
	}

	for _, body := range []string{
		`{"destination":"piwheels.org","ruleType":"IP-CIDR","target":"DIRECT"}`,
		`{"destination":"192.0.2.1","ruleType":"DOMAIN-SUFFIX","target":"DIRECT"}`,
		`{"destination":"2001:db8::1","ruleType":"IP-CIDR","target":"DIRECT"}`,
	} {
		w := httptest.NewRecorder()
		a.handleRuleOverride(w, httptest.NewRequest(http.MethodPost, "/api/rules/override", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid match type returned %d: %s", w.Code, w.Body.String())
		}
	}
}
