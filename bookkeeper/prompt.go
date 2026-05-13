package bookkeeper

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/llm"
)

// PromptRenderer builds provider-neutral messages for a bookkeeping
// reasoning turn. It reads the seeded ledger so the LLM sees the actual
// active chart of accounts, open periods, and known branches -- the rules
// in the system prompt are also enforced by accounting.Validator, so this
// renderer never carries judgment the validator does not also enforce.
//
// Wire it into the llm/openai adapter via openai.Config.Renderer so the
// bookkeeping use case never imports a provider SDK.
type PromptRenderer struct {
	Ledger *accounting.Ledger
}

func (r PromptRenderer) Render(input llm.ReasoningInput) ([]llm.Message, error) {
	if r.Ledger == nil {
		return nil, fmt.Errorf("bookkeeper: prompt renderer has no ledger")
	}

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

	fmt.Fprintf(&b, "\n\nCompany: %s\n", r.Ledger.Company.Name)

	b.WriteString("\nActive chart of accounts:\n")
	b.WriteString(r.activeAccounts())

	b.WriteString("\nOpen accounting periods:\n")
	b.WriteString(r.openPeriods())

	if branches := r.branches(); branches != "" {
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
	codes := make([]string, 0, len(r.Ledger.Accounts))
	for code := range r.Ledger.Accounts {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	var b strings.Builder
	for _, code := range codes {
		a := r.Ledger.Accounts[code]
		if !a.Active {
			continue
		}
		fmt.Fprintf(&b, "  - %s %s (%s)\n", a.Code, a.Name, a.Type)
	}
	return b.String()
}

func (r PromptRenderer) openPeriods() string {
	ids := make([]string, 0, len(r.Ledger.Periods))
	for id := range r.Ledger.Periods {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	for _, id := range ids {
		p := r.Ledger.Periods[id]
		if p.Status != accounting.PeriodOpen {
			continue
		}
		fmt.Fprintf(&b, "  - %s [%s .. %s]\n", p.ID, p.Start.Format("2006-01-02"), p.End.Format("2006-01-02"))
	}
	return b.String()
}

func (r PromptRenderer) branches() string {
	ids := make([]string, 0, len(r.Ledger.Branches))
	for id := range r.Ledger.Branches {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	for _, id := range ids {
		br := r.Ledger.Branches[id]
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
