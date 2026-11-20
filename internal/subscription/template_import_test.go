package subscription

import (
	"strings"
	"testing"
)

func TestTemplateImportPreservesRulesAndProvenance(t *testing.T) {
	tests := []struct {
		format, content string
		rules           int
	}{
		{"rule-list", "# source\nDOMAIN-SUFFIX,example.com,DIRECT\nIP-CIDR,192.0.2.0/24,PROXY", 2},
		{"mihomo-yaml", "proxies: []\nrules:\n - DOMAIN,example.com,REJECT\n - MATCH,PROXY\n", 1},
		{"sing-box-json", `{"route":{"rules":[{"domain_suffix":["example.com","example.net"],"outbound":"direct"},{"ip_cidr":["192.0.2.0/24"],"action":"reject"}],"final":"proxy"}}`, 3},
		{"project-json", `{"id":"old","client":"sing-box","version":1,"mode":"config","preset":"proxy","rules":[{"type":"domain","value":"example.com","target":"DIRECT"}]}`, 1},
	}
	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			result, err := ImportTemplate(TemplateImport{ID: "imported", Client: "sing-box", Format: test.format, Content: test.content, SourceURL: "https://example.test/template", Revision: "commit-123"})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Template.Rules) != test.rules || result.Template.ID != "imported" || result.Template.Origin.Digest != Digest([]byte(test.content)) || result.Template.Origin.Revision != "commit-123" {
				t.Fatal(result)
			}
		})
	}
}
func TestTemplateImportRejectsLossyAndExecutableInputs(t *testing.T) {
	for _, test := range []struct{ format, content string }{
		{"mihomo-yaml", "rules: [DOMAIN-SUFFIX, example.com]"},
		{"mihomo-yaml", "rules:\n - DOMAIN,example.com,custom-policy"},
		{"mihomo-yaml", "rules: []\n---\nrules: []"},
		{"rule-list", "DOMAIN,example.com,DIRECT,no-resolve"},
		{"rule-list", "MATCH,PROXY\nDOMAIN,example.com,DIRECT"},
		{"rule-list", "ruleset=Proxy,https://example.com/rules"},
		{"sing-box-json", `{"route":{"rules":[{"domain":["example.com"],"ip_cidr":["192.0.2.0/24"],"outbound":"proxy"}]}}`},
		{"sing-box-json", `{"route":{"rules":[{"type":"logical","mode":"and","rules":[],"outbound":"proxy"}]}}`},
		{"project-json", `{"id":"x","client":"sing-box","version":1,"mode":"config","script":"alert(1)"}`},
		{"script", "alert(1)"},
	} {
		if _, err := ImportTemplate(TemplateImport{ID: "template", Client: "sing-box", Format: test.format, Content: test.content}); err == nil {
			t.Fatalf("accepted lossy input: %s", test.content)
		}
	}
	if _, err := ImportTemplate(TemplateImport{ID: "template", Client: "sing-box", Format: "rule-list", Content: strings.Repeat("x", 512<<10+1)}); err == nil {
		t.Fatal("unbounded import")
	}
}
