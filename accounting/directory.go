package accounting

import (
	"context"
	"sort"
	"strings"
)

// AccountFilter narrows the chart of accounts for a query. The zero filter
// matches every account in the chart.
//
//   - NameContains is a case-insensitive substring matched against both the
//     account name and the account code.
//   - Type, when non-empty, restricts the result to one account type.
//   - ActiveOnly drops inactive accounts.
type AccountFilter struct {
	NameContains string
	Type         AccountType
	ActiveOnly   bool
}

// AccountLister is the narrow read access FindAccounts needs over a chart
// of accounts. LedgerRepository satisfies it, so callers pass the
// repository they already hold.
type AccountLister interface {
	Accounts(ctx context.Context) ([]Account, error)
}

// FindAccounts returns the accounts in repo's chart that match filter, ordered
// by code. It never mutates the repository.
//
// It lets an agent look up the codes it needs instead of being handed the whole
// chart in its prompt; the bookkeeping tool layer wraps it as a callable tool.
func FindAccounts(ctx context.Context, repo AccountLister, filter AccountFilter) ([]Account, error) {
	all, err := repo.Accounts(ctx)
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(strings.TrimSpace(filter.NameContains))

	var matched []Account
	for _, a := range all {
		if filter.ActiveOnly && !a.Active {
			continue
		}
		if filter.Type != "" && a.Type != filter.Type {
			continue
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(a.Name), needle) &&
			!strings.Contains(strings.ToLower(a.Code), needle) {
			continue
		}
		matched = append(matched, a)
	}

	sort.Slice(matched, func(i, j int) bool { return matched[i].Code < matched[j].Code })
	return matched, nil
}
