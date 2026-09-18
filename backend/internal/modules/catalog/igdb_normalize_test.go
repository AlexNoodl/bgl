package catalog

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

func int64ptr(v int64) *int64 { return &v }

const eldenRingPayloadJSON = `{
	"id": 119133,
	"name": "Elden Ring",
	"slug": "elden-ring",
	"summary": "Elden Ring is an action RPG developed by FromSoftware.",
	"storyline": "Elden Ring takes place in the Lands Between.",
	"first_release_date": 1645747200,
	"cover": {"id": 212094, "image_id": "co4jni"},
	"genres": [
		{"id": 12, "name": "Role-playing (RPG)", "slug": "role-playing-rpg"},
		{"id": 31, "name": "Adventure", "slug": "adventure"}
	],
	"platforms": [
		{"id": 169, "name": "Xbox Series X|S", "slug": "series-x-s"},
		{"id": 48, "name": "PlayStation 4", "slug": "ps4--1"},
		{"id": 6, "name": "PC (Microsoft Windows)", "slug": "win"},
		{"id": 167, "name": "PlayStation 5", "slug": "ps5"},
		{"id": 49, "name": "Xbox One", "slug": "xboxone"}
	],
	"involved_companies": [
		{
			"id": 334813,
			"company": {"id": 248, "name": "Bandai Namco Entertainment", "slug": "bandai-namco-entertainment"},
			"developer": false, "publisher": true, "porting": false, "supporting": false
		},
		{
			"id": 334814,
			"company": {"id": 1012, "name": "FromSoftware", "slug": "fromsoftware"},
			"developer": true, "publisher": true, "porting": false, "supporting": false
		}
	]
}`

func mustUnmarshalPayload(t *testing.T, raw string) IGDBGamePayload {
	t.Helper()
	var p IGDBGamePayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshaling fixture: %v", err)
	}
	return p
}

func sortedRoles(roles []CompanyRole) []CompanyRole {
	out := append([]CompanyRole(nil), roles...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Company.IGDBID != out[j].Company.IGDBID {
			return out[i].Company.IGDBID < out[j].Company.IGDBID
		}
		return out[i].Role < out[j].Role
	})
	return out
}

func TestNormalizeIGDBGame_MainGameWithMultiplePlatformsAndCompanies(t *testing.T) {
	raw := mustUnmarshalPayload(t, eldenRingPayloadJSON)

	got := NormalizeIGDBGame(raw)

	if got.IGDBID != 119133 || got.Title != "Elden Ring" || got.Slug != "elden-ring" {
		t.Fatalf("identity fields wrong: %+v", got)
	}
	if got.GameType != "main_game" {
		t.Errorf("GameType = %q, want main_game (category was absent/zero)", got.GameType)
	}
	if got.ParentGameIGDBID != nil {
		t.Errorf("ParentGameIGDBID = %v, want nil for a main game", got.ParentGameIGDBID)
	}
	if got.CoverImageURL != "https://images.igdb.com/igdb/image/upload/t_cover_big/co4jni.jpg" {
		t.Errorf("CoverImageURL = %q", got.CoverImageURL)
	}
	wantRelease := time.Unix(1645747200, 0).UTC()
	if got.ReleaseDate == nil || !got.ReleaseDate.Equal(wantRelease) {
		t.Errorf("ReleaseDate = %v, want %v", got.ReleaseDate, wantRelease)
	}

	if len(got.Platforms) != 5 {
		t.Fatalf("Platforms = %+v, want 5 entries", got.Platforms)
	}
	if len(got.Genres) != 2 {
		t.Fatalf("Genres = %+v, want 2 entries", got.Genres)
	}

	wantCompanies := []CompanyRole{
		{Company: GameRef{IGDBID: 248, Name: "Bandai Namco Entertainment", Slug: "bandai-namco-entertainment"}, Role: "publisher"},
		{Company: GameRef{IGDBID: 1012, Name: "FromSoftware", Slug: "fromsoftware"}, Role: "developer"},
		{Company: GameRef{IGDBID: 1012, Name: "FromSoftware", Slug: "fromsoftware"}, Role: "publisher"},
	}
	gotCompanies := sortedRoles(got.Companies)
	wantCompanies = sortedRoles(wantCompanies)
	if !reflect.DeepEqual(gotCompanies, wantCompanies) {
		t.Errorf("Companies = %+v, want %+v", gotCompanies, wantCompanies)
	}
}

func TestNormalizeIGDBGame_DLC(t *testing.T) {
	raw := IGDBGamePayload{
		ID:         240009,
		Name:       "Elden Ring: Shadow of the Erdtree",
		Slug:       "elden-ring-shadow-of-the-erdtree",
		Category:   1, // dlc_addon
		ParentGame: int64ptr(119133),
	}

	got := NormalizeIGDBGame(raw)

	if got.GameType != "dlc" {
		t.Errorf("GameType = %q, want dlc", got.GameType)
	}
	if got.ParentGameIGDBID == nil || *got.ParentGameIGDBID != 119133 {
		t.Errorf("ParentGameIGDBID = %v, want 119133", got.ParentGameIGDBID)
	}
}

func TestNormalizeIGDBGame_Remaster(t *testing.T) {
	raw := IGDBGamePayload{
		ID:         81085,
		Name:       "Dark Souls: Remastered",
		Slug:       "dark-souls-remastered",
		Category:   9, // remaster
		ParentGame: int64ptr(2155),
	}

	got := NormalizeIGDBGame(raw)

	if got.GameType != "remaster" {
		t.Errorf("GameType = %q, want remaster", got.GameType)
	}
	if got.ParentGameIGDBID == nil || *got.ParentGameIGDBID != 2155 {
		t.Errorf("ParentGameIGDBID = %v, want 2155", got.ParentGameIGDBID)
	}
}

func TestGameTypeFromIGDBCategory(t *testing.T) {
	cases := []struct {
		category int
		want     string
	}{
		{0, "main_game"},
		{1, "dlc"}, // dlc_addon
		{2, "expansion"},
		{3, "bundle"},
		{4, "standalone_expansion"},
		{5, "dlc"}, // mod
		{6, "dlc"}, // episode
		{7, "dlc"}, // season
		{8, "remake"},
		{9, "remaster"},
		{10, "edition"},    // expanded_game
		{11, "remaster"},   // port
		{12, "remake"},     // fork
		{13, "dlc"},        // pack
		{14, "dlc"},        // update
		{999, "main_game"}, // unknown code falls back
	}

	for _, c := range cases {
		if got := gameTypeFromIGDBCategory(c.category); got != c.want {
			t.Errorf("gameTypeFromIGDBCategory(%d) = %q, want %q", c.category, got, c.want)
		}
	}
}

func TestNormalizeIGDBGame_InvolvedCompanies_SupportingAloneIsDropped(t *testing.T) {
	raw := IGDBGamePayload{
		ID:   1,
		Name: "Some Game",
		Slug: "some-game",
		InvolvedCompanies: []igdbInvolvedCompany{
			{
				Company:    igdbRef{ID: 1, Name: "Support Studio", Slug: "support-studio"},
				Supporting: true,
			},
		},
	}

	got := NormalizeIGDBGame(raw)

	if len(got.Companies) != 0 {
		t.Errorf("Companies = %+v, want none (supporting-only doesn't map to a role)", got.Companies)
	}
}

func TestNormalizeIGDBGame_PortingCountsAsDeveloper(t *testing.T) {
	raw := IGDBGamePayload{
		ID:   1,
		Name: "Some Game",
		Slug: "some-game",
		InvolvedCompanies: []igdbInvolvedCompany{
			{
				Company: igdbRef{ID: 2, Name: "Porting Studio", Slug: "porting-studio"},
				Porting: true,
			},
		},
	}

	got := NormalizeIGDBGame(raw)

	want := []CompanyRole{{Company: GameRef{IGDBID: 2, Name: "Porting Studio", Slug: "porting-studio"}, Role: "developer"}}
	if !reflect.DeepEqual(got.Companies, want) {
		t.Errorf("Companies = %+v, want %+v", got.Companies, want)
	}
}

func TestNormalizeIGDBGame_NoReleaseDateOrCover(t *testing.T) {
	raw := IGDBGamePayload{ID: 1, Name: "Unreleased Game", Slug: "unreleased-game"}

	got := NormalizeIGDBGame(raw)

	if got.ReleaseDate != nil {
		t.Errorf("ReleaseDate = %v, want nil", got.ReleaseDate)
	}
	if got.CoverImageURL != "" {
		t.Errorf("CoverImageURL = %q, want empty", got.CoverImageURL)
	}
}
