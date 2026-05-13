package bookkeeper

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/llm"
)

// PromptRenderer builds provider-neutral messages for a bookkeeping
// reasoning turn. It carries snapshots of the company, chart of accounts,
// periods, and branches so Render itself is synchronous and free of
// repository I/O on the hot path. If the chart of accounts changes
// mid-session, construct a new renderer with NewPromptRenderer.
//
// Wire it into the llm/openai adapter via openai.Config.Renderer so the
// bookkeeping use case never imports a provider SDK.
type PromptRenderer struct {
	Company  accounting.Company
	Accounts []accounting.Account
	Periods  []accounting.Period
	Branches []accounting.Branch
}

// NewPromptRenderer reads the chart of accounts, periods, and branches
// from repo once and returns a renderer ready to plug into an LLM
// adapter.
func NewPromptRenderer(ctx context.Context, company accounting.Company, repo accounting.LedgerRepository) (PromptRenderer, error) {
	if repo == nil {
		return PromptRenderer{}, fmt.Errorf("bookkeeper: NewPromptRenderer needs a repository")
	}
	accounts, err := repo.Accounts(ctx)
	if err != nil {
		return PromptRenderer{}, fmt.Errorf("bookkeeper: load accounts: %w", err)
	}
	periods, err := repo.Periods(ctx)
	if err != nil {
		return PromptRenderer{}, fmt.Errorf("bookkeeper: load periods: %w", err)
	}
	branches, err := repo.Branches(ctx)
	if err != nil {
		return PromptRenderer{}, fmt.Errorf("bookkeeper: load branches: %w", err)
	}
	return PromptRenderer{
		Company:  company,
		Accounts: accounts,
		Periods:  periods,
		Branches: branches,
	}, nil
}

func (r PromptRenderer) Render(input llm.ReasoningInput) ([]llm.Message, error) {
	messages := []llm.Message{
		{Role: llm.MessageRoleSystem, Content: bookkeeperSystemPrompt},
		{Role: llm.MessageRoleUser, Content: r.buildUserPrompt(input)},
	}

	for _, event := range input.Events {
		content := fmt.Sprintf("[%s:%s]\n%s", event.Role, event.Kind, strings.TrimSpace(event.Content))
		role := llm.MessageRoleUser
		if event.Role == llm.EventRoleAssistant {
			role = llm.MessageRoleAssistant
		}
		messages = append(messages, llm.Message{Role: role, Content: content})
	}

	return messages, nil
}

func (r PromptRenderer) buildUserPrompt(input llm.ReasoningInput) string {
	var b strings.Builder
	b.WriteString("Bookkeeping request:\n")
	b.WriteString(strings.TrimSpace(input.Task))
	if instr := strings.TrimSpace(input.Instructions); instr != "" {
		b.WriteString("\n\nFeature instructions:\n")
		b.WriteString(instr)
	}

	fmt.Fprintf(&b, "\n\nCompany: %s\n", r.Company.Name)

	b.WriteString("\nActive chart of accounts:\n")
	b.WriteString(r.activeAccounts())

	b.WriteString("\nOpen accounting periods:\n")
	b.WriteString(r.openPeriods())

	if branches := r.branchesText(); branches != "" {
		b.WriteString("\nReporting branches (optional dimension on each line):\n")
		b.WriteString(branches)
	}

	b.WriteString("\nNotes:\n")
	b.WriteString("  - amount is an integer in minor currency units. $100 USD = 10000.\n")
	b.WriteString("  - include at least two lines with one or more debits and one or more credits; total debit must equal total credit.\n")
	b.WriteString("  - date must be RFC3339 (e.g. 2026-05-12T00:00:00Z) and fall inside the chosen period.\n")
	b.WriteString("  - pick account_code only from the active chart of accounts above.\n")
	b.WriteString("  - pick period_id only from the open periods above.\n")

	b.WriteString("\nReturn JSON with this exact shape:\n")
	b.WriteString(`{"evidence":[{"source":"chart_of_accounts","fact":"..."}],"rationale":"...","intent":{"date":"2026-05-12T00:00:00Z","period_id":"<period_id>","currency":"USD","description":"...","lines":[{"account_code":"<code>","side":"debit","amount":10000,"memo":"...","dimensions":{"branch_id":"<branch_id>"}},{"account_code":"<code>","side":"credit","amount":10000,"memo":"...","dimensions":{}}]}}`)

	return b.String()
}

func (r PromptRenderer) activeAccounts() string {
	sorted := append([]accounting.Account(nil), r.Accounts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Code < sorted[j].Code })

	var b strings.Builder
	for _, a := range sorted {
		if !a.Active {
			continue
		}
		fmt.Fprintf(&b, "  - %s %s (%s)\n", a.Code, a.Name, a.Type)
	}
	return b.String()
}

func (r PromptRenderer) openPeriods() string {
	sorted := append([]accounting.Period(nil), r.Periods...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var b strings.Builder
	for _, p := range sorted {
		if p.Status != accounting.PeriodOpen {
			continue
		}
		fmt.Fprintf(&b, "  - %s [%s .. %s]\n", p.ID, p.Start.Format("2006-01-02"), p.End.Format("2006-01-02"))
	}
	return b.String()
}

func (r PromptRenderer) branchesText() string {
	sorted := append([]accounting.Branch(nil), r.Branches...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var b strings.Builder
	for _, br := range sorted {
		fmt.Fprintf(&b, "  - %s (%s)\n", br.ID, br.Name)
	}
	return b.String()
}

const bookkeeperSystemPrompt = `You are a bookkeeping reasoning engine in a validated agent harness.
Rules you must follow:
- Propose a typed accounting.JournalIntent for the requested transaction.
- Include at least two lines; total debit must equal total credit.
- Use only active account_code values from the chart of accounts.
- Reference an open period_id and a date inside that period.
- Use the same currency on the whole entry.
- If validation feedback is present, fix only the named problems and resubmit.
- Output JSON only. No prose outside the JSON object.`
