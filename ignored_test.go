package main

import "testing"

func TestIgnoredChanges(t *testing.T) {
	from := map[string]string{
		"usr/lib/.build-id/aa/1":            "H:x",
		"usr/lib/.build-id/bb/2":            "H:y",
		"usr/lib64/a.pyc":                   "H:p",
		"usr/lib/sysimage/rpm/rpmdb.sqlite": "H:db1",
	}
	to := map[string]string{
		"usr/lib/.build-id/aa/1":            "H:x2",  // modified
		"usr/lib/.build-id/cc/3":            "H:z",   // added (bb/2 removed)
		"usr/lib64/a.pyc":                   "H:p",   // unchanged -> not counted
		"usr/lib/sysimage/rpm/rpmdb.sqlite": "H:db2", // modified
	}
	got := ignoredChanges(from, to, []string{"usr/lib/.build-id/"}, true)

	// build-id: aa modified + bb removed + cc added = 3; rpm-db: 1; pyc: unchanged -> absent
	if len(got) != 2 {
		t.Fatalf("got %d buckets: %+v", len(got), got)
	}
	if got[0].Label != "usr/lib/.build-id/" || got[0].Count != 3 {
		t.Errorf("bucket[0] = %+v, want {usr/lib/.build-id/ 3}", got[0])
	}
	if got[1].Label != "rpm-db" || got[1].Count != 1 {
		t.Errorf("bucket[1] = %+v, want {rpm-db 1}", got[1])
	}
}
