package accounts

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Acme Research":       "acme-research",
		"  Acme   Research  ": "acme-research",
		"ACME":                "acme",
		"Ünïcodé Names":       "n-cod-names",
		"a":                   "",
		"":                    "",
		"---":                 "",
		"Team 42":             "team-42",
		"person@example.com":  "person-example-com",
		"Jude's workspace":    "jude-s-workspace",
		"!!!":                 "",
		"A very long workspace name that goes well past the sixty three character limit": "a-very-long-workspace-name-that-goes-well-past-the-sixty-three",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Fatalf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSlugifyAlwaysProducesAValidSlug(t *testing.T) {
	inputs := []string{
		"Acme Research",
		"Team 42",
		"person@example.com",
		"A very long workspace name that goes well past the sixty three character limit",
		"----leading and trailing----",
	}
	for _, input := range inputs {
		slug := Slugify(input)
		if slug == "" {
			continue
		}
		if !ValidSlug(slug) {
			t.Fatalf("Slugify(%q) = %q, which the database would reject", input, slug)
		}
	}
}

func TestValidSlug(t *testing.T) {
	valid := []string{"ab", "acme", "acme-research", "a1", "team-42"}
	for _, slug := range valid {
		if !ValidSlug(slug) {
			t.Fatalf("ValidSlug(%q) = false", slug)
		}
	}
	invalid := []string{"", "a", "-acme", "Acme", "acme_research", "acme research", "acme.research"}
	for _, slug := range invalid {
		if ValidSlug(slug) {
			t.Fatalf("ValidSlug(%q) = true", slug)
		}
	}
}

func TestSlugWithSuffixStaysValid(t *testing.T) {
	for _, base := range []string{"ab", "acme", "a-very-long-base-name-that-uses-most-of-the-sixty-three-charact"} {
		slug, err := slugWithSuffix(base)
		if err != nil {
			t.Fatalf("slugWithSuffix(%q): %v", base, err)
		}
		if !ValidSlug(slug) {
			t.Fatalf("slugWithSuffix(%q) = %q, which the database would reject", base, slug)
		}
	}
}

func TestPersonalWorkspaceName(t *testing.T) {
	cases := []struct {
		user User
		want string
	}{
		{User{Name: "Jude"}, "Jude's workspace"},
		{User{Email: "person@example.com"}, "person's workspace"},
		{User{Phone: "+256700000000"}, fallbackWorkspaceName},
		{User{}, fallbackWorkspaceName},
	}
	for _, tc := range cases {
		if got := personalWorkspaceName(tc.user); got != tc.want {
			t.Fatalf("personalWorkspaceName(%+v) = %q, want %q", tc.user, got, tc.want)
		}
	}
}
