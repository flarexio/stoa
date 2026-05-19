package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/harness/loop"
)

const toolFindAccounts = "find_accounts"

// findAccountsArgs is the typed parameter shape for the find_accounts tool; the
// model supplies it as JSON and the handler decodes it here.
type findAccountsArgs struct {
	NameContains string `json:"name_contains"`
	Type         string `json:"type"`
}

// accountTools returns the tool handlers the bookkeeping agent exposes, keyed
// by tool name for loop.Runner.Tools.
func accountTools(repo accounting.LedgerRepository) map[string]loop.ToolHandler {
	return map[string]loop.ToolHandler{
		toolFindAccounts: findAccountsHandler(repo),
	}
}

// findAccountsHandler answers a find_accounts call by searching repo's chart.
// Only active accounts are returned -- a posting may not use an inactive one.
func findAccountsHandler(repo accounting.LedgerRepository) loop.ToolHandler {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var p findAccountsArgs
		if len(args) > 0 {
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid find_accounts args: %w", err)
			}
		}
		matches, err := accounting.FindAccounts(ctx, repo, accounting.AccountFilter{
			NameContains: p.NameContains,
			Type:         accounting.AccountType(strings.TrimSpace(p.Type)),
			ActiveOnly:   true,
		})
		if err != nil {
			return "", err
		}
		return formatAccountMatches(matches), nil
	}
}

// formatAccountMatches renders matched accounts as the text the model reads on
// its next turn.
func formatAccountMatches(accounts []accounting.Account) string {
	if len(accounts) == 0 {
		return "No active accounts match. Try a broader search term."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d matching active account(s):", len(accounts))
	for _, a := range accounts {
		fmt.Fprintf(&b, "\n  - %s %s (%s)", a.Code, a.Name, a.Type)
	}
	return b.String()
}
