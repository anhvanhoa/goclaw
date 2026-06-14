package cmd

import "testing"

func TestBuiltinToolSeedDataIncludesWait(t *testing.T) {
	t.Parallel()
	for _, def := range builtinToolSeedData() {
		if def.Name != "wait" {
			continue
		}
		if def.Category != "runtime" {
			t.Fatalf("wait category = %q, want runtime", def.Category)
		}
		if !def.Enabled {
			t.Fatal("wait should be enabled by default")
		}
		return
	}
	t.Fatal("builtinToolSeedData() missing wait")
}

func TestBuiltinToolSeedDataIncludesWorkstationExec(t *testing.T) {
	t.Parallel()
	for _, def := range builtinToolSeedData() {
		if def.Name != "workstation_exec" {
			continue
		}
		if def.Category != "runtime" {
			t.Fatalf("workstation_exec category = %q, want runtime", def.Category)
		}
		if !def.Enabled {
			t.Fatal("workstation_exec should be enabled by default")
		}
		if len(def.Requires) != 1 || def.Requires[0] != "workstations" {
			t.Fatalf("workstation_exec requires = %#v, want [workstations]", def.Requires)
		}
		return
	}
	t.Fatal("builtinToolSeedData() missing workstation_exec")
}
