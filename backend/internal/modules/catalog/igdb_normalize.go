package catalog

import (
	"fmt"
	"time"
)

type IGDBGamePayload struct {
	ID                int64                 `json:"id"`
	Name              string                `json:"name"`
	Slug              string                `json:"slug"`
	Summary           string                `json:"summary"`
	Storyline         string                `json:"storyline"`
	FirstReleaseDate  int64                 `json:"first_release_date"`
	Category          int                   `json:"category"`
	ParentGame        *int64                `json:"parent_game"`
	Platforms         []igdbRef             `json:"platforms"`
	Genres            []igdbRef             `json:"genres"`
	InvolvedCompanies []igdbInvolvedCompany `json:"involved_companies"`
	Cover             *igdbCover            `json:"cover"`
	Screenshots       []igdbCover           `json:"screenshots"`
}

type igdbRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type igdbInvolvedCompany struct {
	Company    igdbRef `json:"company"`
	Developer  bool    `json:"developer"`
	Publisher  bool    `json:"publisher"`
	Porting    bool    `json:"porting"`
	Supporting bool    `json:"supporting"`
}

type igdbCover struct {
	ImageID string `json:"image_id"`
}

type GameRef struct {
	IGDBID int64
	Name   string
	Slug   string
}

type CompanyRole struct {
	Company GameRef
	Role    string // "developer" or "publisher"
}

type NormalizedGame struct {
	IGDBID           int64
	Title            string
	Slug             string
	Summary          string
	Description      string // IGDB's "storyline"
	ReleaseDate      *time.Time
	GameType         string
	ParentGameIGDBID *int64
	CoverImageURL    string
	Platforms        []GameRef
	Genres           []GameRef
	Companies        []CompanyRole
}

var igdbCategoryToGameType = map[int]string{
	0:  "main_game",
	1:  "dlc", // dlc_addon
	2:  "expansion",
	3:  "bundle",
	4:  "standalone_expansion",
	5:  "dlc", // mod
	6:  "dlc", // episode
	7:  "dlc", // season
	8:  "remake",
	9:  "remaster",
	10: "edition",  // expanded_game
	11: "remaster", // port
	12: "remake",   // fork
	13: "dlc",      // pack
	14: "dlc",      // update
}

func gameTypeFromIGDBCategory(category int) string {
	if gt, ok := igdbCategoryToGameType[category]; ok {
		return gt
	}
	return "main_game"
}

// igdbImageURL builds a full image URL from an IGDB image_id and one of
// IGDB's documented size segments (e.g. "cover_big", "screenshot_big").
func igdbImageURL(imageID, sizeSegment string) string {
	return fmt.Sprintf("https://images.igdb.com/igdb/image/upload/t_%s/%s.jpg", sizeSegment, imageID)
}

func igdbCoverImageURL(imageID string) string {
	return igdbImageURL(imageID, "cover_big")
}

func NormalizeIGDBGame(raw IGDBGamePayload) NormalizedGame {
	n := NormalizedGame{
		IGDBID:           raw.ID,
		Title:            raw.Name,
		Slug:             raw.Slug,
		Summary:          raw.Summary,
		Description:      raw.Storyline,
		GameType:         gameTypeFromIGDBCategory(raw.Category),
		ParentGameIGDBID: raw.ParentGame,
		Platforms:        normalizeRefs(raw.Platforms),
		Genres:           normalizeRefs(raw.Genres),
		Companies:        normalizeCompanies(raw.InvolvedCompanies),
	}

	if raw.FirstReleaseDate > 0 {
		t := time.Unix(raw.FirstReleaseDate, 0).UTC()
		n.ReleaseDate = &t
	}

	if raw.Cover != nil && raw.Cover.ImageID != "" {
		n.CoverImageURL = igdbCoverImageURL(raw.Cover.ImageID)
	}

	return n
}

func normalizeRefs(refs []igdbRef) []GameRef {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(refs))
	out := make([]GameRef, 0, len(refs))
	for _, r := range refs {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		out = append(out, GameRef{IGDBID: r.ID, Name: r.Name, Slug: r.Slug})
	}
	return out
}

type companyRoleKey struct {
	companyID int64
	role      string
}

func normalizeCompanies(raw []igdbInvolvedCompany) []CompanyRole {
	if len(raw) == 0 {
		return nil
	}

	seen := make(map[companyRoleKey]bool)
	var out []CompanyRole
	add := func(ref GameRef, role string) {
		key := companyRoleKey{ref.IGDBID, role}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, CompanyRole{Company: ref, Role: role})
	}

	for _, ic := range raw {
		ref := GameRef{IGDBID: ic.Company.ID, Name: ic.Company.Name, Slug: ic.Company.Slug}
		if ic.Developer || ic.Porting {
			add(ref, "developer")
		}
		if ic.Publisher {
			add(ref, "publisher")
		}
	}

	return out
}
