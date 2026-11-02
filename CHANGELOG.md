# Changelog

## 0.2.0 (2026-11-02)

* Built against API version 0.2.0
* Ingest unchanged

### ⚠ BREAKING CHANGES

* **sdk:** these response types gain required members, so code that constructs them, such as a test double, must set them. The Python SDK refuses a response without them, so it needs an API that sends them (API version 0.2.0). The API now lists at most 100 entries (1,000 clients) in these lists, and orders manual_lines and partially_covered_line_items by amount.
* **sdk:** ImportPreview gains a required member, so code that constructs one, such as a test double, must set it. The Python SDK refuses a preview response without it, so it needs an API that sends it (API version 0.2.0). The API now refuses event text over 200 characters.
* **sdk-go:** EventsService.Ingest is now Ingest(ctx, events, opts ...RequestOption). Calls compile unchanged, but a method value or an interface declaring the old signature must add the variadic parameter.

### Features

* **sdk-go:** add the request layer, per-request options and pagination
* **sdk-go:** call every operation through a service per API group
* **sdk-go:** generate the wire types from the spec and decode money exactly
* **sdk:** add ignored_columns_omitted to the import preview
* **sdk:** count the entries each capped response list leaves out
* **sdk:** list a calculation's reports as summaries without disclosures


### Bug Fixes

* **sdk:** pack ingest batches into bodies of at most 4,000,000 bytes

## 0.1.0 (2026-10-29)

* Built against API version 0.1.0
* First release

### Miscellaneous Chores

* **sdk:** publish the SDKs from their public repositories
