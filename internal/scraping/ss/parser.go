// Package ss scrapes ss.com, a classifieds site with one category tree
// (real-estate/flats/..., transport/cars/..., ...). Every category page is a
// 30-row table; what differs is the columns, which the page names in its own
// header row. The parser reads columns by that header, so a new category
// usually needs one more entry in columns, not a new parser.
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

var (
	// \s does not match a no-break space, which the site may use as a thousands separator.
	numberRe = regexp.MustCompile(`\d[\d\s\x{00a0}.,]*`)
	engineRe = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)\s*(\p{L}*)$`)
)

// column says how to read one table column, keyed by its Latvian header.
type column func(a *ad, cell *goquery.Selection)

// ad collects what columns find; build turns it into the event.
type ad struct {
	events.ListingDiscovered
	district, address string
	make, model       string
}

var columns = map[string]column{
	// Real estate. The address column is "Iela" (street) in district feeds and
	// "Pagasts"/"Rajons" (area<br>street) in city-wide ones.
	"Iela":    func(a *ad, c *goquery.Selection) { a.address = text(c) },
	"Pagasts": func(a *ad, c *goquery.Selection) { a.district, a.address = splitDistrict(c) },
	"Rajons":  func(a *ad, c *goquery.Selection) { a.district, a.address = splitDistrict(c) },
	"Ist.":    num("rooms"),
	"m²":      num("area_m2"),
	"Stāvs":   floor, // "2/4": floor 2 of 4
	"Sērija":  str("series"),

	// Cars. On the all-makes feed "Marka" is "Opel<br>Zafira"; on a make's
	// feed "Modelis" is just the model; a model's feed has neither.
	"Marka": func(a *ad, c *goquery.Selection) {
		switch parts := splitLines(c); len(parts) {
		case 0:
		case 1:
			a.make = parts[0]
		default:
			a.make, a.model = parts[0], strings.Join(parts[1:], " ")
		}
	},
	"Modelis":    func(a *ad, c *goquery.Selection) { a.model = text(c) },
	"Gads":       num("year"),
	"Tilpums":    engine,
	"Nobraukums": mileage,

	// Every category.
	"Cena": func(a *ad, c *goquery.Selection) {
		a.Price = text(c)
		a.PriceValue = priceValue(a.Price)
	},
	"Cena, m2": nil, // derivable from price and area; not worth storing
}

// Parse extracts advertisements from a category page. It fails when the page
// has no header row or a row doesn't match it, which is how a site redesign
// shows up, rather than guessing at columns.
func Parse(r io.Reader, now time.Time) ([]events.ListingDiscovered, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	headers := doc.Find("tr#head_line a.a18")
	if headers.Length() == 0 {
		return nil, fmt.Errorf("no column headers found: page layout changed?")
	}
	var names []string
	headers.Each(func(_ int, h *goquery.Selection) { names = append(names, text(h)) })

	var ads []events.ListingDiscovered
	var rowErr error
	doc.Find("tr[id^='tr_']").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		id, _ := row.Attr("id")
		a, ok, err := parseRow(row, strings.TrimPrefix(id, "tr_"), names, now)
		if err != nil {
			rowErr = fmt.Errorf("row %s: %w", id, err)
			return false
		}
		if !ok {
			slog.Debug("skipped row", "id", id)
			return true
		}
		ads = append(ads, a)
		return true
	})
	return ads, rowErr
}

func parseRow(row *goquery.Selection, externalID string, names []string, now time.Time) (events.ListingDiscovered, bool, error) {
	if externalID == "" || externalID == bannerRowID {
		return events.ListingDiscovered{}, false, nil
	}
	msg := row.Find("td.msg2").First()
	link := msg.Find("a.am").First()
	if msg.Length() == 0 || link.Length() == 0 {
		return events.ListingDiscovered{}, false, nil
	}

	// Cells after the description line up with the headers after "datums".
	cells := msg.NextAll()
	if cells.Length() != len(names) {
		return events.ListingDiscovered{}, false, fmt.Errorf("%d cells but %d headers %v", cells.Length(), len(names), names)
	}

	a := &ad{ListingDiscovered: events.ListingDiscovered{
		Source:     Source,
		ExternalID: externalID,
		Title:      text(link),
		URL:        absolute(attr(link, "href")),
		PostedAt:   now, // list view has no exact post time
		Attrs:      map[string]any{},
	}}
	a.Location = locationSlug(a.URL)
	if img := row.Find("img.isfoto").First(); img.Length() > 0 {
		a.ImageURL = absolute(attr(img, "src"))
	}

	for i, name := range names {
		if h, ok := columns[name]; !ok {
			slog.Debug("unknown column, ignored", "column", name)
		} else if h != nil {
			h(a, cells.Eq(i))
		}
	}
	return a.build(), true, nil
}

func (a *ad) build() events.ListingDiscovered {
	a.resolveCar()
	l := a.ListingDiscovered
	for k, v := range map[string]string{"district": a.district, "address": a.address, "make": a.make, "model": a.model} {
		if v != "" {
			l.Attrs[k] = v
		}
	}
	l.Fields = displayFields(l, a)
	return l
}

func displayFields(l events.ListingDiscovered, a *ad) []events.Field {
	var fs []events.Field
	add := func(name, value string, inline bool) {
		if value != "" {
			fs = append(fs, events.Field{Name: name, Value: value, Inline: inline})
		}
	}
	// Zero rooms/area show up for "Citi"/unknown; nothing useful to display.
	n := func(key string) string {
		if v, ok := l.Num(key); ok && v != 0 {
			return strconv.Itoa(int(v))
		}
		return ""
	}
	s := func(key string) string { v, _ := l.Str(key); return v }

	add("📍 Location", strings.Join(nonEmpty(a.district, a.address), ", "), false)
	add("🚗 Car", strings.Join(nonEmpty(a.make, a.model), " "), false)
	add("📅 Year", n("year"), true)
	if v, ok := l.Num("engine_l"); ok {
		add("⚙️ Engine", fmt.Sprintf("%.1f %s", v, s("fuel")), true)
	}
	if v, ok := l.Num("mileage_km"); ok {
		add("🛣️ Mileage", fmt.Sprintf("%d km", int(v)), true)
	}
	add("🚪 Rooms", n("rooms"), true)
	if v := n("area_m2"); v != "" {
		add("📐 Area", v+" m²", true)
	}
	add("🏢 Series", s("series"), true)
	return fs
}

// floor reads "2/4" as floor 2 in a house of 4 floors.
func floor(a *ad, c *goquery.Selection) {
	at, of, _ := strings.Cut(text(c), "/")
	if n, err := strconv.Atoi(strings.TrimSpace(at)); err == nil {
		a.Attrs["floor"] = float64(n)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(of)); err == nil {
		a.Attrs["floors"] = float64(n)
	}
}

func num(key string) column {
	return func(a *ad, c *goquery.Selection) {
		if n, err := strconv.Atoi(text(c)); err == nil {
			a.Attrs[key] = float64(n)
		}
	}
}

func str(key string) column {
	return func(a *ad, c *goquery.Selection) {
		if v := text(c); v != "" {
			a.Attrs[key] = v
		}
	}
}

// fuelSuffix maps the engine column's suffix ("3.0D") to the search form's
// fuel names. No suffix is petrol; petrol/gas looks the same on the list, so
// a petrol/gas search also accepts plain petrol (see CompileSearch).
var fuelSuffix = map[string]string{"": "Benzīns", "D": "Dīzelis", "H": "Hibrīds", "E": "Elektriskais"}

// engine reads "3.0D" as 3.0 litres, fuel "Dīzelis".
func engine(a *ad, c *goquery.Selection) {
	m := engineRe.FindStringSubmatch(text(c))
	if m == nil {
		return
	}
	if v, err := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64); err == nil {
		a.Attrs["engine_l"] = v
		if fuel, ok := fuelSuffix[strings.ToUpper(m[2])]; ok {
			a.Attrs["fuel"] = fuel
		} else {
			a.Attrs["fuel"] = m[2] // unknown suffix: keep what the site wrote
		}
	}
}

// resolveCar fills make and model from the ad's category path where the
// table has no column for them, and spells both the way the codebook (the
// search form) does, so filters written in those names match.
func (a *ad) resolveCar() {
	rest, ok := strings.CutPrefix(a.Location, CarsCategory+"/")
	if !ok {
		return
	}
	cb, err := CarsCodebook()
	if err != nil {
		return
	}
	slugs := strings.Split(rest, "/")
	m, found := cb.MakeByName(a.make)
	if !found {
		m, found = cb.MakeBySlug(slugs[0])
	}
	if found {
		a.make = m.Name
	}
	if a.model == "" && len(slugs) > 1 {
		a.model = slugs[1]
	}
	if found && a.model != "" {
		if o, ok := m.Model(a.model); ok {
			a.model = o.Name
		}
	}
}

// mileage reads "250 tūkst." (thousand km) or a plain "182 000".
func mileage(a *ad, c *goquery.Selection) {
	t := text(c)
	digits := strings.Join(strings.FieldsFunc(numberRe.FindString(t), func(r rune) bool { return r < '0' || r > '9' }), "")
	n, err := strconv.Atoi(digits)
	if err != nil {
		return
	}
	if strings.Contains(t, "tūkst") {
		n *= 1000
	}
	a.Attrs["mileage_km"] = float64(n)
}

// splitLines splits a cell on its <br>s. goquery's Text() would glue the
// parts together, so each <br> is turned into a newline first.
func splitLines(cell *goquery.Selection) []string {
	clone := cell.Clone()
	clone.Find("br").ReplaceWithHtml("\n")
	return nonEmpty(strings.Split(clone.Text(), "\n")...)
}

// splitDistrict splits an "area<br>street" cell.
func splitDistrict(cell *goquery.Selection) (district, address string) {
	parts := splitLines(cell)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", parts[0] // a lone value is the street
	default:
		return parts[0], parts[1]
	}
}

// locationSlug turns ".../msg/lv/transport/cars/audi/a4/abc.html" into
// "transport/cars/audi/a4": the category path of an ad, which is also the
// prefix every feed covering it shares.
func locationSlug(u string) string {
	_, rest, ok := strings.Cut(u, "/msg/lv/")
	if !ok {
		return ""
	}
	i := strings.LastIndex(rest, "/")
	if i < 0 {
		return ""
	}
	return rest[:i]
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

func text(s *goquery.Selection) string { return strings.TrimSpace(s.Text()) }

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// priceValue pulls the number out of "1 200  €/mēn.", "16.86 €" or "6,900 €".
// A comma followed by exactly three digits is a thousands separator on the
// car pages; any other comma is a decimal one.
func priceValue(s string) *float64 {
	m := numberRe.FindString(s)
	if m == "" {
		return nil
	}
	m = strings.Join(strings.Fields(m), "") // drop spaces and nbsp
	m = strings.TrimRight(m, ".,")
	if i := strings.LastIndex(m, ","); i >= 0 {
		if len(m)-i-1 == 3 {
			m = strings.ReplaceAll(m, ",", "")
		} else {
			m = strings.Replace(m, ",", ".", 1)
		}
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return nil
	}
	return &v
}
