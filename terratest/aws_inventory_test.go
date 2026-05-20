package test

import "testing"

func TestAWSInventoryPrefixesSkipsOtherOwners(t *testing.T) {
	panel := &localControlPanel{}
	records := []panelRunRecord{
		{RunID: "owned-run", AWSPrefix: "own-12345678-aa", Owner: "Test Owner"},
		{RunID: "other-run", AWSPrefix: "oth-12345678-bb", Owner: "Other Owner"},
		{RunID: "legacy-run", AWSPrefix: "legacy-r1", Owner: ""},
	}

	prefixes, runByPrefix := panel.awsInventoryPrefixes(records, " Test   Owner ")

	if got, want := prefixes, []string{"legacy-r1", "own-12345678-aa"}; !sameStrings(got, want) {
		t.Fatalf("prefixes = %v, want %v", got, want)
	}
	if _, ok := runByPrefix["oth-12345678-bb"]; ok {
		t.Fatalf("non-matching owner prefix was included: %#v", runByPrefix)
	}
}

func TestAWSInventoryMatchesRejectsDifferentOwner(t *testing.T) {
	collector := &awsInventoryCollector{
		owner:    "Test Owner",
		prefixes: []string{"oth-12345678-bb", "own-12345678-aa"},
	}

	if collector.matches("oth-12345678-bb-h1", map[string]string{
		"ManagedBy": "rancher-runway",
		"Owner":     "Other Owner",
	}) {
		t.Fatal("resource with a different Owner tag matched by prefix and ManagedBy tag")
	}

	if collector.matches("unrelated", map[string]string{"ManagedBy": "rancher-runway"}) {
		t.Fatal("ManagedBy-only resource matched while owner scoping is configured")
	}

	if !collector.matches("unrelated", map[string]string{"Owner": " Test   Owner "}) {
		t.Fatal("resource with matching Owner tag did not match")
	}

	if !collector.matches("own-12345678-aa-h1", map[string]string{}) {
		t.Fatal("legacy untagged resource with a matching recorded prefix did not match")
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
