package store

import (
	"path/filepath"
	"testing"
)

func TestSaveMergesConcurrentChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.yaml")
	a, _ := Load(path)
	_ = a.Add(&User{Slug: "a", Step: StepCreated})
	_ = a.Add(&User{Slug: "b", Step: StepCreated})
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}

	// two processes load the same state and change different users
	p1, _ := Load(path)
	p2, _ := Load(path)
	ua, _ := p1.Get("a")
	ua.Advance(StepRegistered)
	ub, _ := p2.Get("b")
	ub.Advance(StepEmailVerified)
	if err := p1.Save(ua); err != nil {
		t.Fatal(err)
	}
	if err := p2.Save(ub); err != nil {
		t.Fatal(err)
	}

	final, _ := Load(path)
	ga, _ := final.Get("a")
	gb, _ := final.Get("b")
	if ga.Step != StepRegistered || gb.Step != StepEmailVerified {
		t.Fatalf("merge lost changes: a=%s b=%s", ga.Step, gb.Step)
	}

	// removal survives a merge
	_ = final.Remove("a")
	if err := final.Save(); err != nil {
		t.Fatal(err)
	}
	again, _ := Load(path)
	if _, err := again.Get("a"); err == nil {
		t.Fatal("a should be removed")
	}
	if len(again.Dir.Users) != 1 {
		t.Fatalf("want 1 user, got %d", len(again.Dir.Users))
	}
}

func TestValidSlug(t *testing.T) {
	for _, ok := range []string{"ws1", "asvic12", "a.b-c_d"} {
		if err := ValidSlug(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "WS1", "a b", "-x", "a@b"} {
		if err := ValidSlug(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
