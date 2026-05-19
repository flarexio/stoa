package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flarexio/stoa/accounting"
	"github.com/flarexio/stoa/accounting/agent"
	"github.com/flarexio/stoa/accounting/usecase"
	"github.com/flarexio/stoa/llm"
	"github.com/flarexio/stoa/messaging/inproc"
	"github.com/flarexio/stoa/persistence/memory"
)

type fakeEngineFunc func(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error)

func (f fakeEngineFunc) Predict(ctx context.Context, input llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
	return f(ctx, input)
}

// awsBillRepo seeds an in-memory repository from the testdata fixture.
func awsBillRepo(t *testing.T) accounting.LedgerRepository {
	t.Helper()
	scenario, err := accounting.LoadScenarioFile("../../testdata/accounting/aws_bill.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	repo := memory.NewAccountingRepository()
	if err := scenario.Seed(context.Background(), repo); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return repo
}

// awsBillScenario returns the scenario alongside a seeded repository so
// tests that need scenario.Company (the prompt renderer tests) do not
// have to reload the file.
func awsBillScenario(t *testing.T) (accounting.Scenario, accounting.LedgerRepository) {
	t.Helper()
	scenario, err := accounting.LoadScenarioFile("../../testdata/accounting/aws_bill.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	repo := memory.NewAccountingRepository()
	if err := scenario.Seed(context.Background(), repo); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return scenario, repo
}

// wireBus subscribes the standard apply handler so the bus's published
// events land in the repo's projection. Returned as a convenience for
// test setup.
func wireBus(t *testing.T, repo accounting.LedgerRepository) usecase.EventBus {
	t.Helper()
	bus := inproc.NewAccountingBus()
	if err := bus.Subscribe(usecase.EventHandlerFunc(func(ctx context.Context, evt accounting.JournalPosted) error {
		return repo.Apply(ctx, evt)
	})); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return bus
}

func balancedAWSIntent() accounting.JournalIntent {
	return accounting.JournalIntent{
		Date:        time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
		PeriodID:    "2026-05",
		Currency:    "USD",
		Description: "Paid AWS bill on company credit card",
		Lines: []accounting.JournalLine{
			{AccountCode: "5200", Side: accounting.SideDebit, Amount: 10000, Dimensions: accounting.Dimensions{BranchID: "hq"}},
			{AccountCode: "2100", Side: accounting.SideCredit, Amount: 10000, Dimensions: accounting.Dimensions{BranchID: "hq"}},
		},
	}
}

// postCmd wraps a JournalIntent as the post_journal Command the fake
// engine hands back to the agent.
func postCmd(intent accounting.JournalIntent) usecase.Command {
	return usecase.Command{Kind: usecase.CommandPostJournal, Post: &intent}
}

func fixedClock() time.Time {
	return time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
}

func TestAgent_PostsBalancedJournal(t *testing.T) {
	repo := awsBillRepo(t)
	bus := wireBus(t, repo)

	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		return llm.ReasoningResult[usecase.Command]{
			Rationale: "AWS invoice paid on credit card; expense debit, liability credit",
			Intent:    postCmd(balancedAWSIntent()),
		}, nil
	})

	agent := agent.Bookkeeper{
		Engine:    engine,
		Repo:      repo,
		Publisher: bus,
		Clock:     fixedClock,
		MaxTurns:  3,
	}
	res, err := agent.Book(context.Background(), "Paid AWS bill 100 USD using company credit card")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.Turns != 1 {
		t.Fatalf("expected 1 turn, got %d", res.Turns)
	}
	if res.Command.Kind != usecase.CommandPostJournal {
		t.Fatalf("expected a post_journal command, got %q", res.Command.Kind)
	}
	if res.Entry.ID == "" {
		t.Fatal("expected posted entry to be returned")
	}
	if !strings.HasPrefix(res.Entry.ID, "JE-") {
		t.Fatalf("unexpected entry id format: %q", res.Entry.ID)
	}
	got, err := repo.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != res.Entry.ID {
		t.Fatalf("expected one stored entry matching returned ID, got %+v", got)
	}
}

func TestAgent_CorrectsAfterValidationFeedback(t *testing.T) {
	repo := awsBillRepo(t)
	bus := wireBus(t, repo)

	calls := 0
	engine := fakeEngineFunc(func(_ context.Context, input llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		calls++
		switch calls {
		case 1:
			intent := balancedAWSIntent()
			intent.Lines[1].Amount = 9000
			return llm.ReasoningResult[usecase.Command]{
				Rationale: "first pass: assume $90 surcharge waived",
				Intent:    postCmd(intent),
			}, nil
		default:
			sawValidationErr := false
			for _, e := range input.Events {
				if e.Kind == llm.EventValidationError {
					sawValidationErr = true
				}
			}
			if !sawValidationErr {
				t.Errorf("expected validation_error event on retry, got events %+v", input.Events)
			}
			return llm.ReasoningResult[usecase.Command]{
				Rationale: "corrected: rebalance credit to match $100 debit",
				Intent:    postCmd(balancedAWSIntent()),
			}, nil
		}
	})

	agent := agent.Bookkeeper{Engine: engine, Repo: repo, Publisher: bus, Clock: fixedClock, MaxTurns: 3}
	res, err := agent.Book(context.Background(), "Paid AWS bill 100 USD using company credit card")
	if err != nil {
		t.Fatalf("expected success after correction, got %v", err)
	}
	if res.Turns != 2 {
		t.Fatalf("expected 2 turns, got %d", res.Turns)
	}
	if calls != 2 {
		t.Fatalf("expected engine called twice, got %d", calls)
	}
	entries, _ := repo.Entries(context.Background())
	if len(entries) != 1 {
		t.Fatalf("expected exactly one entry posted after correction, got %d", len(entries))
	}
}

func TestAgent_RejectsClosedPeriodIntent(t *testing.T) {
	repo := awsBillRepo(t)
	bus := wireBus(t, repo)

	calls := 0
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		calls++
		intent := balancedAWSIntent()
		if calls == 1 {
			intent.PeriodID = "2026-04"
		}
		return llm.ReasoningResult[usecase.Command]{
			Rationale: "best guess",
			Intent:    postCmd(intent),
		}, nil
	})

	agent := agent.Bookkeeper{Engine: engine, Repo: repo, Publisher: bus, Clock: fixedClock, MaxTurns: 3}
	res, err := agent.Book(context.Background(), "Record April AWS bill late")
	if err != nil {
		t.Fatalf("expected success after correcting to open period, got %v", err)
	}
	if res.Entry.PeriodID != "2026-05" {
		t.Fatalf("expected entry posted to open period, got %q", res.Entry.PeriodID)
	}
}

func TestAgent_SequentialIDsAcrossPosts(t *testing.T) {
	repo := awsBillRepo(t)
	bus := wireBus(t, repo)

	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		return llm.ReasoningResult[usecase.Command]{Intent: postCmd(balancedAWSIntent())}, nil
	})
	agent := agent.Bookkeeper{Engine: engine, Repo: repo, Publisher: bus, Clock: fixedClock, MaxTurns: 3}

	a, err := agent.Book(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	b, err := agent.Book(context.Background(), "second")
	if err != nil {
		t.Fatal(err)
	}
	if a.Entry.ID == b.Entry.ID {
		t.Fatalf("expected distinct IDs across posts, got %s and %s", a.Entry.ID, b.Entry.ID)
	}
}

// TestAgent_ReversesAPostedEntry drives two Book calls through one agent:
// the first posts an entry, the second proposes a reverse_journal command
// for it. It proves the agent routes both commands of the union -- post
// and reverse -- through the same registry-backed loop.
func TestAgent_ReversesAPostedEntry(t *testing.T) {
	ctx := context.Background()
	repo := awsBillRepo(t)
	bus := wireBus(t, repo)

	postedID := ""
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		if postedID == "" {
			return llm.ReasoningResult[usecase.Command]{Intent: postCmd(balancedAWSIntent())}, nil
		}
		return llm.ReasoningResult[usecase.Command]{
			Rationale: "reverse the entry the request names",
			Intent: usecase.Command{
				Kind:    usecase.CommandReverseJournal,
				Reverse: &usecase.ReverseCommand{EntryID: postedID, Reason: "duplicate posting"},
			},
		}, nil
	})
	agent := agent.Bookkeeper{Engine: engine, Repo: repo, Publisher: bus, Clock: fixedClock, MaxTurns: 3}

	first, err := agent.Book(ctx, "Paid AWS bill on the company credit card")
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	postedID = first.Entry.ID

	second, err := agent.Book(ctx, "Reverse "+postedID+"; it was a duplicate")
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if second.Command.Kind != usecase.CommandReverseJournal {
		t.Fatalf("expected the reverse_journal command, got %q", second.Command.Kind)
	}
	if second.Entry.ID == first.Entry.ID {
		t.Fatal("expected the reversal to be posted as a new entry")
	}
	if !strings.HasPrefix(second.Entry.Description, "Reversal of "+first.Entry.ID) {
		t.Fatalf("expected the reversal description to name the original, got %q", second.Entry.Description)
	}
	entries, _ := repo.Entries(ctx)
	if len(entries) != 2 {
		t.Fatalf("expected the original and the reversal stored, got %d", len(entries))
	}
}

func TestAgent_ClosedPeriodMidSessionBlocksFurtherPosts(t *testing.T) {
	ctx := context.Background()
	repo := awsBillRepo(t)
	bus := wireBus(t, repo)

	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		return llm.ReasoningResult[usecase.Command]{Intent: postCmd(balancedAWSIntent())}, nil
	})
	agent := agent.Bookkeeper{Engine: engine, Repo: repo, Publisher: bus, Clock: fixedClock, MaxTurns: 1}

	if _, err := agent.Book(ctx, "first post should succeed"); err != nil {
		t.Fatalf("first post: %v", err)
	}

	// Close the period directly through the repository's seed path.
	period, _, _ := repo.Period(ctx, "2026-05")
	period.Status = accounting.PeriodClosed
	if err := repo.PutPeriod(ctx, period); err != nil {
		t.Fatalf("close period: %v", err)
	}

	if _, err := agent.Book(ctx, "second post against closed period"); err == nil {
		t.Fatal("expected error after closing the period")
	}
}

func TestAgent_MissingEngine(t *testing.T) {
	repo := awsBillRepo(t)
	bus := wireBus(t, repo)
	agent := agent.Bookkeeper{Repo: repo, Publisher: bus}
	if _, err := agent.Book(context.Background(), "x"); err == nil {
		t.Fatal("expected error for missing engine")
	}
}

func TestAgent_MissingRepo(t *testing.T) {
	bus := inproc.NewAccountingBus()
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		return llm.ReasoningResult[usecase.Command]{}, nil
	})
	agent := agent.Bookkeeper{Engine: engine, Publisher: bus}
	if _, err := agent.Book(context.Background(), "x"); err == nil {
		t.Fatal("expected error for missing repository")
	}
}

func TestAgent_MissingPublisher(t *testing.T) {
	repo := awsBillRepo(t)
	engine := fakeEngineFunc(func(_ context.Context, _ llm.ReasoningInput) (llm.ReasoningResult[usecase.Command], error) {
		return llm.ReasoningResult[usecase.Command]{}, nil
	})
	agent := agent.Bookkeeper{Engine: engine, Repo: repo}
	if _, err := agent.Book(context.Background(), "x"); err == nil {
		t.Fatal("expected error for missing publisher")
	}
}
