package imagepath

import "testing"

// For is the one rule that decides where an object lands in the bucket. Both the
// pulsar and the kafka processor call it, and a wrong answer here is not a
// visible failure -- it is an upload that reports success into a path nothing
// serves, which is exactly how a prefix bug once hid for a week.
func TestFor(t *testing.T) {
	const id = "5f26a95f-0000-4000-8000-000000000001"

	tests := []struct {
		name     string
		dataType string
		id       string
		objName  string
		wantPath string
		wantOK   bool
	}{
		{"anime lands at the root", TypeAnime, id, "", "/" + id, true},
		{"character", TypeCharacter, id, "", "/characters/" + id, true},
		{"staff", TypeStaff, id, "", "/staff/" + id, true},
		{"banner", TypeBanner, id, "", "/banners/" + id, true},
		{"poster is its own path, not the root", TypePoster, id, "", "/posters/" + id, true},

		// Name is only a fallback for messages published before producers sent
		// ids: in-flight during a rolling deploy, and the retry topic.
		{"falls back to name when there is no id", TypeAnime, "", "Cowboy Bebop", "/Cowboy+Bebop", true},
		{"prefers id over name", TypeBanner, id, "Cowboy Bebop", "/banners/" + id, true},
		{"escapes a fallback name", TypeAnime, "", "a/b c", "/a%2Fb+c", true},

		{"unknown type is refused", "Sticker", id, "", "", false},
		{"empty type is refused", "", id, "", "", false},
		{"nothing to key on is refused", TypeAnime, "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := For(tt.dataType, tt.id, tt.objName)
			if ok != tt.wantOK {
				t.Fatalf("For(%q, %q, %q) ok = %v, want %v", tt.dataType, tt.id, tt.objName, ok, tt.wantOK)
			}
			if got != tt.wantPath {
				t.Fatalf("For(%q, %q, %q) = %q, want %q", tt.dataType, tt.id, tt.objName, got, tt.wantPath)
			}
		})
	}
}

// The root object and /posters/ hold different assets for the same anime -- the
// scraper's MyAnimeList image and TheTVDB's high-resolution poster. Collapsing
// them would have the poster sync overwrite every anime's existing image.
func TestPosterDoesNotCollideWithAnimeRoot(t *testing.T) {
	const id = "5f26a95f-0000-4000-8000-000000000001"

	root, ok := For(TypeAnime, id, "")
	if !ok {
		t.Fatal("anime path not ok")
	}
	poster, ok := For(TypePoster, id, "")
	if !ok {
		t.Fatal("poster path not ok")
	}
	if root == poster {
		t.Fatalf("anime and poster resolved to the same path %q", root)
	}
}
