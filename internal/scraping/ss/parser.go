// Package ss scrapes real estate listings from ss.com.
package ss

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

const (
	Source  = "ss.com"
	baseURL = "https://www.ss.com"

	// bannerRowID is an ad-banner row that looks like a listing row.
	bannerRowID = "bnr_712"
)

var priceRe = regexp.MustCompile(`\d+(?:\s*\d+)*`)

// Parse extracts advertisements from a search-result page.
func Parse(r io.Reader, now time.Time) ([]events.ListingDiscovered, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var ads []events.ListingDiscovered
	doc.Find("tr[id^='tr_']").Each(func(_ int, row *goquery.Selection) {
		id, _ := row.Attr("id")
		ad, ok := parseRow(row, strings.TrimPrefix(id, "tr_"), now)
		if !ok {
			slog.Debug("skipped row", "id", id)
			return
		}
		ads = append(ads, ad)
	})
	return ads, nil
}

func parseRow(row *goquery.Selection, externalID string, now time.Time) (events.ListingDiscovered, bool) {
	if externalID == "" || externalID == bannerRowID {
		return events.ListingDiscovered{}, false
	}

	msg := row.Find("td.msg2").First()
	if msg.Length() == 0 {
		return events.ListingDiscovered{}, false
	}

	link := msg.Find("a.am").First()
	if link.Length() == 0 {
		return events.ListingDiscovered{}, false
	}

	ad := events.ListingDiscovered{
		Source:     Source,
		ExternalID: externalID,
		Title:      strings.TrimSpace(link.Text()),
		URL:        absolute(attr(link, "href")),
		Category:   strings.TrimSpace(msg.Find("div.ads_cat_names").First().Text()),
		PostedAt:   now, // list view has no exact post time
	}

	if img := row.Find("img.isfoto").First(); img.Length() > 0 {
		ad.HasPhoto = true
		ad.ImageURL = absolute(attr(img, "src"))
	}

	// Columns: district<br>address | rooms | area | series | price
	cells := row.Find("td.msga2-o")
	if cells.Length() >= 5 {
		ad.District, ad.Address = splitDistrict(cells.Eq(0))
		ad.Rooms = atoi(cells.Eq(1).Text())
		ad.AreaM2 = atoi(cells.Eq(2).Text())
		ad.Series = strings.TrimSpace(cells.Eq(3).Text())
		ad.Price = strings.TrimSpace(cells.Eq(4).Text())
		ad.PriceValue = priceValue(ad.Price)
	}

	return ad, true
}

// splitDistrict splits a "district<br>address" cell. goquery's Text() would
// glue the two parts together, so the <br> is turned into a newline first.
func splitDistrict(cell *goquery.Selection) (district, address string) {
	clone := cell.Clone()
	clone.Find("br").ReplaceWithHtml("\n")

	var parts []string
	for _, p := range strings.Split(clone.Text(), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	default:
		return parts[0], parts[1]
	}
}

func absolute(u string) string {
	if u != "" && !strings.HasPrefix(u, "http") {
		return baseURL + u
	}
	return u
}

func attr(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return v
}

func atoi(s string) *int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &n
}

// priceValue pulls the number out of strings like "1 200  €/mēn." -> 1200.
func priceValue(s string) *float64 {
	m := priceRe.FindString(s)
	if m == "" {
		return nil
	}
	v, err := strconv.ParseFloat(strings.Join(strings.Fields(m), ""), 64)
	if err != nil {
		return nil
	}
	return &v
}
