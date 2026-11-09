package subscriptions

import "testing"

func TestAdminKey(t *testing.T) {
	for entry, want := range map[string]string{
		"discord:123":      "discord:123",
		"  Discord:123 ":   "discord:123",
		`"discord:123"`:    "discord:123",
		"discord: 123":     "discord:123",
		"123":              "", // a bare ID doesn't say which platform
		"discord:":         "",
		":123":             "",
		"telegram:-100777": "telegram:-100777",
	} {
		got, ok := adminKey(entry)
		if want == "" {
			if ok {
				t.Errorf("adminKey(%q) = %q, want rejected", entry, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("adminKey(%q) = %q, %v; want %q", entry, got, ok, want)
		}
	}
}
