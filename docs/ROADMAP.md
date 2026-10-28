# Offer monitoring roadmap

Planned work. Current architecture and setup: [PROJECT_NOTES.md](PROJECT_NOTES.md).

## Direction

One platform to discover offers and track prices across classifieds, marketplaces, and shops.
Support both search-based discovery and pinned-product watches.
Share collection work across users; keep their rules and notification channels separate.
Use the existing repo, dashboard, database, and bot. Keep maintenance low.

No paid data APIs. Prefer permitted free APIs/feeds, then rate-limited scraping.
Do not bypass access controls.

## Next steps

- [ ] Verify real Discord delivery; add Telegram.
- [ ] Schedule stale-data cleanup and add notification burst limits/digests.
- [ ] Add dashboard authentication, user access controls, and search editing.
- [ ] Replace category-path parsing with strongly typed categories across adapters,
  events, storage, API, and TypeScript; backfill existing records and handle unknown categories.

## Discovery at a target price

Find matching offers across configured sources without pinning a URL. The existing matcher
supports price ceilings; marketplace and shop discovery still need source adapters.

- [ ] Expose target-price searches for classifieds, marketplace searches, and shop catalogs:
  e.g. an Audi below EUR 10,000 or a specific Apple Watch below EUR 250 from supported sellers.
- [ ] Keep new matching-listing alerts separate from existing offers dropping below the
  target; the latter needs the price-change tracking below. Deduplicate both alert types.

## Price watches: first milestone

Examples: an Apple Watch below EUR 250, or protein powder on Amazon.de dropping 5%
from its price when the watch starts. Support either rule, not just target prices.

- [ ] Support one reliable retailer and one product URL first. Amazon.de is a candidate,
  not a promised integration.
- [ ] Configure per-user watches: exact offer/variant, target price or percentage drop, currency, interval,
  and destination (`#new-offers`, `#price-drops`, or a personal channel).
- [ ] For percentage rules, capture the current valid price at watch start as a fixed reference
  until manually reset: EUR 100 with a 5% drop rule alerts at EUR 95 or less. Count gradual drops too.
- [ ] Revisit pinned offers independently of discovery feeds; share checks across users.
- [ ] Store timestamped price history using exact monetary values and explicit currency.
- [ ] Save changes and outbox events atomically; deduplicate alerts per watch and change.
- [ ] Start with a silent baseline. Alert when price crosses at-or-below the rule's threshold;
  re-arm after it rises above. No repeated alerts while it stays below.
- [ ] Test both rules, gradual drops, reference resets, retries, restarts, missing prices, and shared watches.

Done when one URL can be watched, its price history inspected, and a qualifying alert
delivered through the existing bot. Scraping adapters also need failure/recovery alerts.

## Monitoring: required for scraping

- [ ] Track last valid success, failures, and stale/paused state per source and target.
- [ ] Detect broken layouts even on HTTP 200; distinguish parser failures, access issues,
  legitimate empty results, and unavailable products. Keep bounded, redacted diagnostics.
- [ ] Retain the last valid data on failure. Never interpret a missing price as zero or
  a broken parser as a discount/removal; validate suspicious changes before user alerts.
- [ ] Send deduplicated admin incidents and recovery alerts to `#monitoring-alerts`.
  Include affected watches, evidence, and a dashboard link; support mute windows and backoff.
- [ ] Flag abnormal result counts, missing fields, price/identity shifts, mass unavailability,
  request/error spikes, stuck queues, notification bursts, and storage growth.
- [ ] Start with configurable thresholds; add per-target rolling baselines after enough data.
  Allow normal fluctuations and real discounts; avoid alert storms and recovery flapping.
- [ ] Show incidents and source health in the dashboard. Add an independent watchdog for
  stopped workers/bot outages, and retry failed admin notifications.
- [ ] Test layout changes, anomalies, false positives, deduplication, and recovery.

## Later

- [ ] Dashboard watch editing, pause/resume, price history, and last-check status; chat commands.
- [ ] Restock rules. Keep advertised discounts separate from observed price drops.
- [ ] Preserve active watches and required price history during cleanup.
- [ ] Add City24, Vinted, and shop/marketplace adapters one at a time, with parser fixtures and health checks.
- [ ] Cross-store comparison: match variant, condition, seller, currency, and shipping/fees first.
  Start with manual product grouping.
- [ ] CI, observability, dead-letter queues, protobuf, scheduler, and deployment tooling as needed.