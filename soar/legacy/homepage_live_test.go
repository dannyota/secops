package legacy

import (
	"context"
	"testing"
)

// homepageReadPage/homepageReadSize are safe pagination constants for the
// Homepage "GetByRequest" list endpoints: page 0 with a large window so a
// freshly created widget is guaranteed to land in the first (only) page.
const (
	homepageReadPage = 0
	homepageReadSize = 1000
)

// TestLiveHomepageReads exercises the read-only Homepage widget endpoints that
// succeed on a tenant with no prior setup: the paged widget lists (attachments,
// contacts, links, notes, RSS) plus the RSS widget count. All take only safe
// pagination constants and an empty search term, so they return cleanly (an
// empty page) even on a blank tenant. Runs under SECOPS_SOAR_SMOKE=1.
//
// The two /cases/homepagecases methods (HomepageListCases, HomepageGetCasesCount)
// are deliberately omitted: they return a server-side HTTP 500.
func TestLiveHomepageReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "homepage/ListAttachments", func() (RawJSON, error) {
		return lc.HomepageListAttachments(ctx, homepageReadPage, homepageReadSize, "")
	})
	readProbe(t, "homepage/ListContacts", func() (RawJSON, error) {
		return lc.HomepageListContacts(ctx, homepageReadPage, homepageReadSize, "")
	})
	readProbe(t, "homepage/ListLinks", func() (RawJSON, error) {
		return lc.HomepageListLinks(ctx, homepageReadPage, homepageReadSize, "")
	})
	readProbe(t, "homepage/ListNotes", func() (RawJSON, error) {
		return lc.HomepageListNotes(ctx, homepageReadPage, homepageReadSize, "")
	})
	readProbe(t, "homepage/ListRss", func() (RawJSON, error) {
		return lc.HomepageListRss(ctx, homepageReadPage, homepageReadSize, "")
	})
	readProbe(t, "homepage/GetRssCount", func() (RawJSON, error) {
		return lc.HomepageGetRssCount(ctx)
	})
}

// GROUP A (safest) — homepage widget CRUD. Each test runs the full lifecycle on
// a throwaway, purely cosmetic homepage widget (a personal landing-page panel
// item) with an explicit create template (so it works on an empty tenant) and a
// unique smoke-label display name. All are write-gated (SECOPS_SOAR_SMOKE_WRITE=1)
// and clean up via t.Cleanup. Chain: list -> create -> list -> read -> edit ->
// read -> delete -> list.

// TestLiveHomepageNoteCRUD — a homepage note (title + free-text body).
func TestLiveHomepageNoteCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind: "homepage-note",
		list: func(ctx context.Context) (RawJSON, error) {
			return lc.HomepageListNotes(ctx, homepageReadPage, homepageReadSize, "")
		},
		idOf:     intField("id"),
		nameOf:   strField("title"),
		rename:   setField("title"),
		template: func() map[string]any { return map[string]any{"note": "secopsctl smoke test note"} },
		create:   func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageCreateNote(ctx, o) },
		update:   func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageUpdateNote(ctx, o) },
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			id, _ := intField("id")(o)
			return lc.HomepageDeleteNote(ctx, id)
		},
	})
}

// TestLiveHomepageLinkCRUD — a homepage quick-link (name + url).
func TestLiveHomepageLinkCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind: "homepage-link",
		list: func(ctx context.Context) (RawJSON, error) {
			return lc.HomepageListLinks(ctx, homepageReadPage, homepageReadSize, "")
		},
		idOf:   intField("id"),
		nameOf: strField("name"),
		rename: setField("name"),
		template: func() map[string]any {
			return map[string]any{"url": "https://example.com/secopsctl-smoke", "description": "secopsctl smoke test"}
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageCreateLink(ctx, o) },
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageUpdateLink(ctx, o) },
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			id, _ := intField("id")(o)
			return lc.HomepageDeleteLink(ctx, id)
		},
	})
}

// TestLiveHomepageContactCRUD — a homepage contact card (contactName + email).
func TestLiveHomepageContactCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind: "homepage-contact",
		list: func(ctx context.Context) (RawJSON, error) {
			return lc.HomepageListContacts(ctx, homepageReadPage, homepageReadSize, "")
		},
		idOf:   intField("id"),
		nameOf: strField("contactName"),
		rename: setField("contactName"),
		template: func() map[string]any {
			return map[string]any{"email": "smoke@example.com", "description": "secopsctl smoke test"}
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageCreateContact(ctx, o) },
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageUpdateContact(ctx, o) },
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			id, _ := intField("id")(o)
			return lc.HomepageDeleteContact(ctx, id)
		},
	})
}

// TestLiveHomepageRssCRUD — a homepage RSS feed widget (title + feed url).
func TestLiveHomepageRssCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind: "homepage-rss",
		list: func(ctx context.Context) (RawJSON, error) {
			return lc.HomepageListRss(ctx, homepageReadPage, homepageReadSize, "")
		},
		idOf:     intField("id"),
		nameOf:   strField("title"),
		rename:   setField("title"),
		template: func() map[string]any { return map[string]any{"feed": "https://example.com/secopsctl-smoke.rss"} },
		create:   func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageCreateRss(ctx, o) },
		update:   func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.HomepageUpdateRss(ctx, o) },
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			id, _ := intField("id")(o)
			return lc.HomepageDeleteRss(ctx, id)
		},
	})
}
