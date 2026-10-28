package ss

import (
	"os"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	f, err := os.Open("testdata/search_result.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ads, err := Parse(f, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// 5 listing rows + 1 banner row that must be skipped.
	if len(ads) != 5 {
		t.Fatalf("got %d ads, want 5", len(ads))
	}

	a := ads[0]
	if a.ExternalID != "58228884" {
		t.Errorf("ExternalID = %q", a.ExternalID)
	}
	if a.Category != "Rīga : Purvciems" {
		t.Errorf("Category = %q", a.Category)
	}
	if a.District != "Rīga" || a.Address != "Vējavas 10k2" {
		t.Errorf("District/Address = %q / %q", a.District, a.Address)
	}
	if a.Rooms == nil || *a.Rooms != 1 {
		t.Errorf("Rooms = %v", a.Rooms)
	}
	if a.AreaM2 == nil || *a.AreaM2 != 33 {
		t.Errorf("AreaM2 = %v", a.AreaM2)
	}
	if a.Series != "602." {
		t.Errorf("Series = %q", a.Series)
	}
	if a.PriceValue == nil || *a.PriceValue != 300 {
		t.Errorf("PriceValue = %v (price %q)", a.PriceValue, a.Price)
	}
	if !a.HasPhoto || a.ImageURL == "" {
		t.Errorf("photo not parsed: %v %q", a.HasPhoto, a.ImageURL)
	}
	if a.URL != "https://www.ss.com/msg/lv/real-estate/flats/riga/purvciems/akbki.html" {
		t.Errorf("URL = %q", a.URL)
	}
	for _, ad := range ads {
		if ad.ExternalID == "bnr_712" {
			t.Error("banner row was not skipped")
		}
	}
}

func TestPriceValue(t *testing.T) {
	for in, want := range map[string]float64{
		"290 €/mēn.":    290,
		"300  €/mēn.":   300,
		"1 200 €/mēn.":  1200,
		"1 200  €/mēn.": 1200,
	} {
		got := priceValue(in)
		if got == nil || *got != want {
			t.Errorf("priceValue(%q) = %v, want %v", in, got, want)
		}
	}
	if priceValue("pārdod") != nil {
		t.Error("expected nil for non-numeric price")
	}
}

func TestParseFormData(t *testing.T) {
	f := ParseFormData("txt=&opt[20][]=17020&btn=Meklēt")
	if f.Get("opt[20][]") != "17020" || f.Get("btn") != "Meklēt" || !f.Has("txt") {
		t.Errorf("unexpected form: %v", f)
	}
}
