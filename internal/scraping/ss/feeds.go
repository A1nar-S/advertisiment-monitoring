package ss

import (
	"slices"
	"strings"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

// A feed is an SS.com category path below /lv/:
//
//	real-estate/flats/riga/centre/hand_over   (rent, one district)
//	real-estate/flats/riga/all/hand_over      (rent, whole city)
//	transport/cars/audi/a4                    (one model)
//	transport/cars/audi                       (whole make)
//
// Real estate ends in a deal ("hand_over" = rent out, "sell", ...) and uses
// "all" in place of a district; other categories are plain paths.
type feed struct {
	scope string // category path without deal and "all"; ads under it are covered
	deal  string // "" outside real estate
}

var deals = map[string]bool{"hand_over": true, "sell": true, "buy": true, "rent": true, "exchange": true}

func parseFeed(s string) feed {
	parts := strings.Split(strings.Trim(s, "/"), "/")
	var f feed
	if n := len(parts); n > 1 && deals[parts[n-1]] {
		f.deal, parts = parts[n-1], parts[:n-1]
	}
	if n := len(parts); n > 0 && parts[n-1] == "all" {
		parts = parts[:n-1]
	}
	f.scope = strings.Join(parts, "/")
	return f
}

// path renders the feed's URL path; the inverse of parseFeed for a scope
// that was collapsed from narrower ones.
func (f feed) path() string {
	if f.deal == "" {
		return f.scope
	}
	return f.scope + "/all/" + f.deal
}

func (f feed) covers(location string) bool {
	return location == f.scope || strings.HasPrefix(location, f.scope+"/")
}

func parentOf(scope string) string {
	i := strings.LastIndex(scope, "/")
	if i < 0 {
		return ""
	}
	return scope[:i]
}

// minCollapseDepth keeps a collapse from climbing to a whole top-level
// category ("transport/cars"), which would be a firehose of unrelated ads.
// Three levels is a city or a make.
const minCollapseDepth = 3

// PlanFeeds: every request counts against SS.com's tolerance, so a feed that
// another wanted feed already contains is dropped, and when more than
// collapseAt feeds share a parent (districts of a city, models of a make) the
// parent is scraped instead. InScope narrows results back to each search.
func (*Client) PlanFeeds(wanted []string, collapseAt int) []string {
	have := map[feed]bool{}
	raw := map[feed]string{}
	for _, w := range wanted {
		f := parseFeed(w)
		have[f] = true
		raw[f] = strings.Trim(w, "/")
	}

	// Drop feeds contained in another wanted feed of the same deal.
	var kept []feed
	for k := range have {
		swallowed := false
		for p := parentOf(k.scope); p != "" && !swallowed; p = parentOf(p) {
			swallowed = have[feed{p, k.deal}]
		}
		if !swallowed {
			kept = append(kept, k)
		}
	}

	// Collapse crowded siblings into their parent.
	siblings := map[feed][]feed{}
	for _, k := range kept {
		p := feed{parentOf(k.scope), k.deal}
		siblings[p] = append(siblings[p], k)
	}
	var out []string
	for p, ks := range siblings {
		if len(ks) > collapseAt && strings.Count(p.scope, "/") >= minCollapseDepth-1 {
			out = append(out, p.path())
			continue
		}
		for _, k := range ks {
			out = append(out, raw[k])
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// InScope checks that an ad lies under one of the wanted feeds and was found
// in the same deal. The deal comes from the feed the ad was scraped on, since
// an ad's own URL has none.
func (*Client) InScope(wanted []string, l events.ListingDiscovered) bool {
	scraped := parseFeed(l.Feed)
	for _, w := range wanted {
		f := parseFeed(w)
		if f.deal == scraped.deal && f.covers(l.Location) {
			return true
		}
	}
	return false
}
