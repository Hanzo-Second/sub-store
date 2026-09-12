package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var destinationLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

func normalizeDestination(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if ip, err := netip.ParseAddr(value); err == nil {
		if ip.Zone() != "" {
			return "", fmt.Errorf("IP zone identifiers are not supported")
		}
		return ip.Unmap().String(), nil
	}
	if len(value) == 0 || len(value) > 253 || strings.Trim(value, "0123456789.") == "" {
		return "", fmt.Errorf("Enter a domain or IP address")
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || !destinationLabel.MatchString(label) {
			return "", fmt.Errorf("Enter a domain or IP address without a scheme, port, or path (use punycode for international domains)")
		}
	}
	return value, nil
}

// known=false preserves uncertainty: a later match cannot bypass an unknown earlier rule.
func destinationMatch(kind, value, destination string) (matched, known bool) {
	ip, ipErr := netip.ParseAddr(destination)
	value = strings.ToLower(strings.TrimSpace(value))
	switch strings.ToUpper(kind) {
	case "MATCH":
		return true, true
	case "DOMAIN":
		return ipErr != nil && destination == value, true
	case "DOMAIN-SUFFIX":
		return ipErr != nil && (destination == value || strings.HasSuffix(destination, "."+value)), true
	case "DOMAIN-KEYWORD":
		return ipErr != nil && strings.Contains(destination, value), true
	case "DOMAIN-WILDCARD":
		if ipErr == nil {
			return false, true
		}
		return wildcardMatch(value, destination)
	case "DOMAIN-REGEX":
		if ipErr == nil {
			return false, true
		}
		expression, err := regexp.Compile("(?i)" + strings.TrimSpace(value))
		if err != nil {
			return false, false
		}
		return expression.MatchString(destination), true
	case "IP-CIDR", "IP-CIDR6":
		if ipErr != nil {
			return false, false
		} // Client DNS resolution may affect routing.
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return false, false
		}
		return prefix.Contains(ip), true
	default:
		return false, false
	}
}

func wildcardMatch(pattern, value string) (bool, bool) {
	var expression strings.Builder
	expression.WriteByte('^')
	for _, character := range pattern {
		switch character {
		case '*':
			expression.WriteString(".*")
		case '?':
			expression.WriteByte('.')
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	expression.WriteByte('$')
	compiled, err := regexp.Compile("(?i)" + expression.String())
	if err != nil {
		return false, false
	}
	return compiled.MatchString(value), true
}

func providerRuleLines(format, content string) ([]string, bool) {
	lines := strings.Split(strings.TrimPrefix(content, "\ufeff"), "\n")
	if format == "yaml" {
		var payload struct {
			Payload []string `yaml:"payload"`
			Rules   []string `yaml:"rules"`
		}
		if yaml.Unmarshal([]byte(content), &payload) != nil || (payload.Payload == nil && payload.Rules == nil) {
			return nil, false
		}
		lines = append(payload.Payload, payload.Rules...)
	} else if format != "text" {
		return nil, false
	}
	return lines, true
}

func (a *App) providerDestinationMatch(name, destination string) (bool, bool) {
	matched, known, _ := a.providerDestinationMatchDetail(name, destination)
	return matched, known
}

func (a *App) providerDestinationMatchDetail(name, destination string) (bool, bool, string) {
	var id int64
	var behavior, format, content string
	if err := a.db.QueryRow(`SELECT id,LOWER(behavior),LOWER(provider_format),COALESCE(content,'') FROM rule_providers WHERE LOWER(name)=LOWER(?) AND enabled=1 ORDER BY id LIMIT 1`, strings.TrimSpace(name)).Scan(&id, &behavior, &format, &content); err != nil {
		return false, false, ""
	}
	// Default providers start without a local snapshot. Fetch the referenced
	// provider on first use so lookup actually checks its rules without making
	// the user visit every provider card and click Update first.
	if content == "" && format != "mrs" {
		body, err := a.refreshRuleProvider(id)
		if err != nil {
			return false, false, ""
		}
		content = string(body)
	}
	lines, parsed := providerRuleLines(format, content)
	if !parsed {
		return false, false, ""
	}
	known := true
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		kind, value := "", line
		switch behavior {
		case "domain":
			matched, certain := providerDomainMatch(value, destination)
			if matched {
				return true, true, line
			}
			known = known && certain
			continue
		case "ipcidr":
			kind = "IP-CIDR"
		case "classical":
			parts := strings.Split(line, ",")
			if len(parts) < 2 {
				known = false
				continue
			}
			kind, value = parts[0], parts[1]
		default:
			return false, false, ""
		}
		matched, certain := destinationMatch(kind, value, destination)
		if matched {
			return true, true, line
		}
		known = known && certain
	}
	return false, known, ""
}

func (a *App) handleRuleLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	destination, err := normalizeDestination(r.URL.Query().Get("destination"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	unresolved := []string{}
	settings := a.settings()
	groups := a.listGroups()
	for _, rule := range effectiveRoutingRules(a.listRules(), a.listServiceRuleGroups(), a.listRuleProviders(), settings.RoutingMode, routingProxyGroupName(settings, groups)) {
		parts := strings.Split(rule.value, ",")
		if len(parts) < 2 {
			unresolved = append(unresolved, rule.value)
			continue
		}
		target := parts[len(parts)-1]
		if strings.EqualFold(parts[0], "MATCH") {
			writeJSON(w, 200, map[string]any{"destination": destination, "target": target, "rule": rule.value, "certain": len(unresolved) == 0, "unresolved": unresolved})
			return
		}
		matched, known := destinationMatch(parts[0], parts[1], destination)
		providerRule := ""
		if strings.EqualFold(parts[0], "RULE-SET") {
			matched, known, providerRule = a.providerDestinationMatchDetail(parts[1], destination)
		}
		if !known {
			unresolved = append(unresolved, rule.value)
		}
		if matched {
			result := map[string]any{"destination": destination, "target": target, "rule": rule.value, "certain": len(unresolved) == 0, "unresolved": unresolved}
			if providerRule != "" {
				result["providerRule"] = providerRule
			}
			writeJSON(w, 200, result)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"destination": destination, "target": "DIRECT", "rule": "MATCH,DIRECT", "certain": len(unresolved) == 0, "unresolved": unresolved})
}

func (a *App) handleRuleOverride(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Destination string `json:"destination"`
		Target      string `json:"target"`
		RuleType    string `json:"ruleType"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	destination, err := normalizeDestination(input.Destination)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	rule := routingRule{Match: destination, Target: input.Target, Enabled: true}
	if ip, err := netip.ParseAddr(destination); err == nil {
		rule.RuleType = "IP-CIDR"
		bits := 32
		if ip.Is6() {
			rule.RuleType = "IP-CIDR6"
			bits = 128
		}
		rule.Match = fmt.Sprintf("%s/%d", destination, bits)
		if input.RuleType != "" && !strings.EqualFold(input.RuleType, rule.RuleType) {
			writeError(w, 400, "The selected match type is not valid for this IP address")
			return
		}
	} else {
		rule.RuleType = strings.ToUpper(strings.TrimSpace(input.RuleType))
		if rule.RuleType == "" {
			rule.RuleType = "DOMAIN"
		}
		if rule.RuleType != "DOMAIN" && rule.RuleType != "DOMAIN-SUFFIX" && rule.RuleType != "DOMAIN-KEYWORD" {
			writeError(w, 400, "Match type must be DOMAIN, DOMAIN-SUFFIX, or DOMAIN-KEYWORD")
			return
		}
	}
	if err := a.resolveRuleTarget(&rule); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if rule.TargetGroupID == 0 && rule.Target != "DIRECT" && rule.Target != "REJECT" {
		writeError(w, 400, "Choose an enabled proxy group, DIRECT, or REJECT")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	// Overrides have their own precedence tier. Reordering ordinary rules cannot
	// move them below a service rule, and editors need no negative priorities.
	var id int64
	err = tx.QueryRow(`SELECT id FROM rules WHERE UPPER(rule_type)=? AND LOWER(match_value)=? ORDER BY is_override DESC,priority,id LIMIT 1`, rule.RuleType, rule.Match).Scan(&id)
	switch err {
	case nil:
		_, err = tx.Exec(`UPDATE rules SET target=?,target_group_id=?,priority=1,enabled=1,is_override=1 WHERE id=?`, rule.Target, nullableID(rule.TargetGroupID), id)
	case sql.ErrNoRows:
		_, err = tx.Exec(`INSERT INTO rules(rule_type,match_value,target,target_group_id,priority,enabled,is_override,created_at) VALUES(?,?,?,?,1,1,1,?)`, rule.RuleType, rule.Match, rule.Target, nullableID(rule.TargetGroupID), time.Now().UTC().Format(time.RFC3339))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true})
}

// Provider wildcards match whole labels, unlike DOMAIN-WILDCARD rules.
// https://wiki.metacubex.one/handbook/syntax/#_8
func providerDomainMatch(pattern, destination string) (bool, bool) {
	if _, err := netip.ParseAddr(destination); err == nil {
		return false, true
	}
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	includeRoot := strings.HasPrefix(pattern, "+.")
	subdomainsOnly := strings.HasPrefix(pattern, ".")
	if includeRoot {
		pattern = strings.TrimPrefix(pattern, "+.")
	}
	if subdomainsOnly {
		pattern = strings.TrimPrefix(pattern, ".")
	}
	labels := strings.Split(pattern, ".")
	domains := strings.Split(destination, ".")
	for _, label := range labels {
		if label != "*" && !destinationLabel.MatchString(label) {
			return false, false
		}
	}
	if len(domains) < len(labels) || (subdomainsOnly && len(domains) == len(labels)) {
		return false, true
	}
	if !includeRoot && !subdomainsOnly && len(domains) != len(labels) {
		return false, true
	}
	offset := len(domains) - len(labels)
	for i, label := range labels {
		if label != "*" && label != domains[offset+i] {
			return false, true
		}
	}
	return true, true
}
