//go:build integration

// Integration tests run the real services in-process against real Postgres and
// Kafka, with a local fake standing in for ss.com and another for Discord.
//
//	go test -tags integration ./internal/app -v
//
// Docker is required: Postgres and Kafka are started with testcontainers. To
// use servers you already run (e.g. `docker compose up -d postgres kafka`),
// set TEST_DATABASE_URL and TEST_KAFKA_BROKERS; each test creates its own
// database and Kafka namespace, so nothing is shared between runs.
package app

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/config"
	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping/ss"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

const (
	seedWindow = 3 * time.Second
	flatsFeed  = "real-estate/flats/riga/centre/hand_over"
	cityFeed   = "real-estate/flats/riga/all/hand_over"
	carsFeed   = "transport/cars/audi/a4"
)

// --- infrastructure ---------------------------------------------------------

type infra struct {
	pgURL   string // a fresh database on the server
	brokers []string
}

var (
	sharedOnce  sync.Once
	sharedPG    string
	sharedKafka []string
	sharedErr   error
)

// startInfra returns Postgres and Kafka, starting containers once per test
// binary unless TEST_DATABASE_URL / TEST_KAFKA_BROKERS point at running ones.
func startInfra(t *testing.T) infra {
	t.Helper()
	sharedOnce.Do(func() {
		if pg, kb := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_KAFKA_BROKERS"); pg != "" && kb != "" {
			sharedPG, sharedKafka = pg, strings.Split(kb, ",")
			return
		}
		ctx := context.Background()
		pgc, err := postgres.Run(ctx, "postgres:15",
			postgres.WithDatabase("postgres"), postgres.WithUsername("postgres"), postgres.WithPassword("test"),
			postgres.BasicWaitStrategies(),
			testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(2*time.Minute)),
		)
		if err != nil {
			sharedErr = fmt.Errorf("start postgres (is Docker running?): %w", err)
			return
		}
		if sharedPG, err = pgc.ConnectionString(ctx, "sslmode=disable"); err != nil {
			sharedErr = err
			return
		}
		kc, err := kafka.Run(ctx, "confluentinc/confluent-local:7.5.0")
		if err != nil {
			sharedErr = fmt.Errorf("start kafka: %w", err)
			return
		}
		if sharedKafka, sharedErr = kc.Brokers(ctx); sharedErr != nil {
			return
		}
		// Containers live until the test binary exits; testcontainers' reaper removes them.
	})
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}

	// A private database per test keeps tests independent.
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, sharedPG)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("it_%d", rand.Uint32())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(sharedPG)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return infra{pgURL: u.String(), brokers: sharedKafka}
}

// --- fakes ------------------------------------------------------------------

// fakeSS serves saved ss.com pages by feed, and lets a test add a new ad on top.
type fakeSS struct {
	mu    sync.Mutex
	pages map[string]string // feed -> html
	hits  map[string]int
	srv   *httptest.Server
}

func newFakeSS(t *testing.T) *fakeSS {
	f := &fakeSS{pages: map[string]string{}, hits: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/lv/"), "/")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits[path]++
		html, ok := f.pages[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, html)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSS) serve(feed, fixture string, t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "scraping", "ss", "testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.pages[feed] = string(b)
	f.mu.Unlock()
}

var rowRe = regexp.MustCompile(`(?s)<tr id="tr_(\d+)">.*?</tr>`)

// addAd puts a copy of an existing ad (likeID, or the first one if empty),
// under a new ID, on top of the feed: what a freshly posted ad looks like.
func (f *fakeSS) addAd(feed, likeID, newID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	html := f.pages[feed]
	var m []string
	for _, c := range rowRe.FindAllStringSubmatch(html, -1) {
		if likeID == "" || c[1] == likeID {
			m = c
			break
		}
	}
	if m == nil {
		panic("no ad " + likeID + " in " + feed)
	}
	row := strings.ReplaceAll(m[0], m[1], newID)
	first := rowRe.FindString(html)
	f.pages[feed] = strings.Replace(html, first, row+first, 1)
}

// fakeDiscord records what the notifier would have posted.
type fakeDiscord struct {
	mu   sync.Mutex
	sent []sent
}

type sent struct{ target, listingID, text string }

func (*fakeDiscord) Kind() string { return "discord" }
func (d *fakeDiscord) NotifyListing(_ context.Context, target string, l events.ListingDiscovered) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, sent{target: target, listingID: l.ExternalID})
	return nil
}
func (d *fakeDiscord) NotifyText(_ context.Context, target, msg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, sent{target: target, text: msg})
	return nil
}

// listings returns the listing notifications, ignoring operational texts.
func (d *fakeDiscord) listings() []sent {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []sent
	for _, s := range d.sent {
		if s.listingID != "" {
			out = append(out, s)
		}
	}
	return out
}

// --- helpers ----------------------------------------------------------------

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// consistently fails if cond ever turns false during d, i.e. "nothing else happened".
func consistently(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if !cond() {
			t.Fatalf("%s did not hold", what)
		}
	}
}

// run starts a service and stops it when the returned func is called (or at test end).
func run(t *testing.T, name string, fn func(ctx context.Context) error) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			if err != nil && ctx.Err() == nil {
				t.Errorf("%s exited with error: %v", name, err)
			}
		case <-time.After(30 * time.Second):
			t.Errorf("%s did not stop", name)
		}
	}
	t.Cleanup(stop)
	return stop
}

func countListings(t *testing.T, pool *pgxpool.Pool) (n int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM listings`).Scan(&n); err != nil {
		return 0 // table may not exist yet
	}
	return n
}

const usersFile = `
users:
  - name: alice
    channels: [{kind: discord, target: alice-chan}]
    searches:
      - name: centre rent
        feeds: [` + flatsFeed + `]
        filters:
          price: {max: 700}
          rooms: {min: 1}
  - name: bob
    channels: [{kind: discord, target: bob-chan}]
    searches:
      - name: big flats in centre
        feeds: [` + flatsFeed + `]
        filters:
          rooms: {min: 3}
`

const moreUsers = usersFile + `
      - name: whole city, cheap
        feeds: [` + cityFeed + `]
        filters:
          price: {max: 450}
  - name: alice-cars
    channels: [{kind: discord, target: alice-cars-chan}]
    searches:
      - name: a4 2010+
        feeds: [` + carsFeed + `]
        filters:
          year: {min: 2010}
          price: {max: 8000}
`

// --- the test ---------------------------------------------------------------

func TestPipeline(t *testing.T) {
	in := startInfra(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, in.pgURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	site := newFakeSS(t)
	site.serve(flatsFeed, "feed_district.html", t)
	site.serve(cityFeed, "feed_city.html", t)
	site.serve(carsFeed, "feed_cars_model.html", t)
	discord := &fakeDiscord{}

	reg := sources.NewRegistry(ss.NewClient(5 * time.Second).WithBaseURL(site.srv.URL))
	common := config.Common{DatabaseURL: in.pgURL, KafkaBrokers: in.brokers, Namespace: fmt.Sprintf("it%d", rand.Uint32())}

	users := filepath.Join(t.TempDir(), "users.yaml")
	if err := os.WriteFile(users, []byte(usersFile), 0o600); err != nil {
		t.Fatal(err)
	}

	scraperCfg := config.Scraper{
		Common: common, Source: "ss.com", CrawlInterval: time.Second, RequestGap: 10 * time.Millisecond, MaxPages: 3, FeedCollapseAt: 3,
	}
	startScraper := func() func() {
		return run(t, "scraper", func(ctx context.Context) error { return Scraper(ctx, scraperCfg, reg) })
	}
	run(t, "ingest", func(ctx context.Context) error {
		return Ingest(ctx, config.Ingest{Common: common, SeedWindow: seedWindow})
	})
	run(t, "matcher", func(ctx context.Context) error {
		return Matcher(ctx, config.Matcher{Common: common, UsersFile: users, FeedCollapseAt: 3}, reg)
	})
	run(t, "notifier", func(ctx context.Context) error {
		return Notifier(ctx, config.Notifier{Common: common, Delay: 0}, discord)
	})

	// 1. First start: the feed's current ads are stored, nobody is notified.
	// The scraper starts last so the matcher has already seeded the searches
	// the scraper reads its feeds from.
	eventually(t, 60*time.Second, "matcher to seed searches", func() bool {
		var n int
		return pool.QueryRow(ctx, `SELECT count(*) FROM searches`).Scan(&n) == nil && n == 2
	})
	stopScraper := startScraper()
	eventually(t, 60*time.Second, "30 ads prefilled", func() bool { return countListings(t, pool) == 30 })
	consistently(t, 4*time.Second, "no notifications while prefilling (several scrape cycles)",
		func() bool { return len(discord.listings()) == 0 })

	// 2. A new ad appears: only the search it satisfies is notified, once.
	// (alice: price<=700, rooms>=1 -> yes. bob: rooms>=3 -> no, the ad has 2.)
	// 58230432 is a centre flat: 2 rooms, 590 EUR/month.
	site.addAd(flatsFeed, "58230432", "99000001")
	eventually(t, 60*time.Second, "alice notified of the new flat", func() bool { return len(discord.listings()) == 1 })
	consistently(t, 3*time.Second, "exactly one notification, no duplicates",
		func() bool { return len(discord.listings()) == 1 })
	if got := discord.listings()[0]; got.target != "alice-chan" || got.listingID != "99000001" {
		t.Fatalf("notification = %+v, want alice-chan / 99000001", got)
	}

	// 3. Restarting the scraper (it forgets what it saw) must not re-announce anything.
	stopScraper()
	stopScraper = startScraper()
	hitsBefore := site.hitCount(flatsFeed)
	eventually(t, 30*time.Second, "restarted scraper to fetch the feed", func() bool { return site.hitCount(flatsFeed) > hitsBefore+1 })
	consistently(t, 2*time.Second, "no re-announcement after scraper restart",
		func() bool { return len(discord.listings()) == 1 })
	_ = stopScraper

	// 4. New searches are added while running: a city-wide one and a cars one.
	// Their feeds are new, so their existing ads are prefilled without notifying,
	// and the city feed replaces the narrower district feed (fewer requests).
	if err := os.WriteFile(users, []byte(moreUsers), 0o600); err != nil {
		t.Fatal(err)
	}
	subs := subscriptions.NewService(pool, reg, 3)
	if err := subs.SeedFromFile(ctx, users); err != nil {
		t.Fatal(err)
	}
	eventually(t, 60*time.Second, "cars feed prefilled", func() bool { return countListings(t, pool) > 30+25 })
	time.Sleep(seedWindow + time.Second) // the new feeds' seed windows end
	consistently(t, 3*time.Second, "new feeds' existing ads did not notify anyone",
		func() bool { return len(discord.listings()) == 1 })

	if before := site.hitCount(flatsFeed); true {
		time.Sleep(3 * time.Second)
		if after := site.hitCount(flatsFeed); after != before {
			t.Errorf("district feed still fetched (%d -> %d) though the city-wide feed covers it", before, after)
		}
	}

	// 5. New ads in the city-wide scrape. A centre flat reaches the district
	// search through InScope; a Pļavnieki flat reaches only the city-wide one;
	// a new car reaches the generic year/price filters.
	site.addAd(cityFeed, "58230432", "99000002")
	eventually(t, 60*time.Second, "alice notified via the city-wide feed", func() bool { return len(discord.listings()) == 2 })
	site.addAd(cityFeed, "58230445", "99000004") // Pļavnieki, 3 rooms, 420
	eventually(t, 60*time.Second, "bob notified by his city-wide search", func() bool { return len(discord.listings()) == 3 })
	site.addAd(carsFeed, "", "99000003") // 2012, 6900 EUR
	eventually(t, 60*time.Second, "car owner notified", func() bool { return len(discord.listings()) == 4 })
	consistently(t, 3*time.Second, "no extra notifications", func() bool { return len(discord.listings()) == 4 })

	got := map[string]string{}
	for _, s := range discord.listings() {
		got[s.listingID] = s.target
	}
	want := map[string]string{
		"99000001": "alice-chan", "99000002": "alice-chan", "99000004": "bob-chan", "99000003": "alice-cars-chan",
	}
	for id, target := range want {
		if got[id] != target {
			t.Errorf("listing %s went to %q, want %q (all: %v)", id, got[id], target, got)
		}
	}
}

func (f *fakeSS) hitCount(feed string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[feed]
}
