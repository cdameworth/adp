package enforcement

import (
	"context"
	"errors"
	"testing"
)

type fakeVerifier struct {
	verified map[string]bool
	err      error
}

func (f fakeVerifier) IsCommitVerified(_ context.Context, sha string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.verified[sha], nil
}

func newReconciler(v CommitVerifier) *Reconciler {
	return NewReconciler(v, NewInMemoryFindingStore(0))
}

func TestObserveCommits_FlagsOnlyUngoverned(t *testing.T) {
	r := newReconciler(fakeVerifier{verified: map[string]bool{"a": true}})
	got, err := r.ObserveCommits(context.Background(), []ObservedCommit{
		{SHA: "a", Repo: "acme/x"}, {SHA: "b", Repo: "acme/x"}, {SHA: "c"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 findings (b,c), got %d", len(got))
	}
	for _, f := range got {
		if f.Type != FindingUngovernedCommit || f.Status != StatusOpen {
			t.Errorf("unexpected finding: %+v", f)
		}
	}
	if all := r.Findings(""); len(all) != 2 {
		t.Fatalf("store should hold 2, got %d", len(all))
	}
}

func TestObserveCommits_Dedup(t *testing.T) {
	r := newReconciler(fakeVerifier{verified: map[string]bool{}})
	_, _ = r.ObserveCommits(context.Background(), []ObservedCommit{{SHA: "b"}})
	_, _ = r.ObserveCommits(context.Background(), []ObservedCommit{{SHA: "b"}})
	if all := r.Findings(""); len(all) != 1 {
		t.Fatalf("same commit should dedup to 1 finding, got %d", len(all))
	}
}

func TestObserveCommits_AllGoverned(t *testing.T) {
	r := newReconciler(fakeVerifier{verified: map[string]bool{"a": true, "b": true}})
	got, _ := r.ObserveCommits(context.Background(), []ObservedCommit{{SHA: "a"}, {SHA: "b"}})
	if len(got) != 0 || len(r.Findings("")) != 0 {
		t.Fatalf("governed commits must not produce findings")
	}
}

func TestObserveCommits_VerifierError(t *testing.T) {
	r := newReconciler(fakeVerifier{err: errors.New("boom")})
	if _, err := r.ObserveCommits(context.Background(), []ObservedCommit{{SHA: "a"}}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestResolveAndStatusFilter(t *testing.T) {
	r := newReconciler(fakeVerifier{verified: map[string]bool{}})
	got, _ := r.ObserveCommits(context.Background(), []ObservedCommit{{SHA: "b"}})
	id := got[0].ID

	if _, ok := r.Resolve(id, StatusResolved); !ok {
		t.Fatal("resolve should succeed")
	}
	if open := r.Findings(StatusOpen); len(open) != 0 {
		t.Errorf("no open findings expected, got %d", len(open))
	}
	if resolved := r.Findings(StatusResolved); len(resolved) != 1 {
		t.Errorf("want 1 resolved, got %d", len(resolved))
	}
	if _, ok := r.Resolve("nope", StatusResolved); ok {
		t.Error("resolving unknown id should fail")
	}
}

func TestInMemoryEviction(t *testing.T) {
	s := NewInMemoryFindingStore(2)
	for _, ref := range []string{"a", "b", "c"} {
		s.Upsert(Finding{ID: "id-" + ref, Type: FindingUngovernedCommit, Reference: ref, Status: StatusOpen})
	}
	if got := len(s.List("")); got != 2 {
		t.Fatalf("bounded store should hold 2, got %d", got)
	}
	if _, ok := s.Get("id-a"); ok {
		t.Error("oldest finding should have been evicted")
	}
}
