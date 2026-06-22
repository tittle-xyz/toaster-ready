// SPDX-License-Identifier: Apache-2.0

package githubclient

import "testing"

func TestSplitSlug(t *testing.T) {
	o, n, ok := splitSlug("octocat/hello-world")
	if !ok || o != "octocat" || n != "hello-world" {
		t.Fatalf("splitSlug(octocat/hello-world) = %q,%q,%v", o, n, ok)
	}
	for _, bad := range []string{"", "bad", "/x", "x/"} {
		if _, _, ok := splitSlug(bad); ok {
			t.Errorf("splitSlug(%q) should be !ok", bad)
		}
	}
}

func TestStubIsNoData(t *testing.T) {
	s := NewStub()
	if r := s.LatestRunGreen("a/b"); !r.NoData {
		t.Error("Stub.LatestRunGreen should be no-data")
	}
	if r := s.BranchProtected("a/b"); !r.NoData {
		t.Error("Stub.BranchProtected should be no-data")
	}
}

// Stub and GoGitHub must both satisfy Client.
var _ Client = Stub{}
var _ Client = (*GoGitHub)(nil)
