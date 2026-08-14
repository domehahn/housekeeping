package providerfactory

import "testing"

func TestRegistry_OnlyGitLabImplemented(t *testing.T) {
	reg := Registry()
	if len(reg) == 0 {
		t.Fatal("expected at least one registered provider")
	}
	for _, d := range reg {
		if d.Type == "gitlab" {
			if d.Status != "implemented" {
				t.Errorf("expected gitlab to be implemented, got %q", d.Status)
			}
			if len(d.RequiredConfig) == 0 {
				t.Error("expected gitlab to list required configuration")
			}
			return
		}
	}
	t.Fatal("gitlab not found in registry")
}

func TestFind(t *testing.T) {
	d, ok := Find("gitlab")
	if !ok || d.Type != "gitlab" {
		t.Errorf("Find(gitlab) = %+v, %v", d, ok)
	}
	if _, ok := Find("bitbucket"); ok {
		t.Error("Find(bitbucket) should report not found - no adapter exists for it")
	}
}
