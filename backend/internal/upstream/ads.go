package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"freebucks-proxy/backend/internal/wirefacts"
)

// freebuffCliUA is the ads-API request User-Agent, mirroring the installed
// official CLI binary the proxy emulates. The CLI composes it from its own
// build-time version (upstream/freebuff
// cli/src/hooks/use-gravity-ad.ts:817-821,
// `Freebuff-CLI/${getCliEnv().CODEBUFF_CLI_VERSION}`; IS_FREEBUFF picks the
// product token), injected from the released Freebuff wrapper version the
// build was invoked with (freebuff/cli/build.ts:23,33 ->
// cli/scripts/build-binary.ts:167-168). Real installs therefore advertise
// versions like Freebuff-CLI/0.0.140
// (common/src/util/ad-user-agent.ts:44-46), never the monorepo's placeholder
// cli/package.json 1.0.0. wirefacts.VendorVersion IS that released wrapper
// version for the snapshots this proxy speaks (generated from
// backend/internal/wirefacts/testdata/wire/snapshots.json), so the version
// claimed on the wire follows the vendored wire at every re-pin instead of
// freezing at a hand-typed literal.
var freebuffCliUA = "Freebuff-CLI/" + wirefacts.VendorVersion

const (
	// maxAdResponseRead caps the ad response body read.
	maxAdResponseRead = 512
	// maxAdAuctionRead caps the successful auction body read: the ads array
	// (creatives + impUrls) exceeds the 512B error-path cap.
	maxAdAuctionRead = 64 << 10
)

// adUserAgents maps runtime.GOOS to the browser-like Chrome-151 UA sent to
// ad providers for targeting/fraud screening (#124). The CLI ships one entry
// per platform (reference common/src/util/ad-user-agent.ts: darwin/win32/
// linux AD_USER_AGENTS built from AD_CHROME_VERSION=151.0.0.0;
// use-gravity-ad.ts sends it as the body userAgent) and warns that native
// runtime UAs look bot-like to ad networks. The body UA must agree with the
// device block's os (deviceOS): a mixed signal (e.g. os:"linux" with a
// Windows UA) reads as spoofing to ad networks.
var adUserAgents = map[string]string{
	"darwin":  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
	"windows": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
	"linux":   "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
}

// adBrowserUserAgent returns the platform-consistent ads body UA for the
// host, falling back to the Linux entry exactly like the CLI's
// getAdUserAgent (AD_USER_AGENTS[platformKey] ?? linux).
func adBrowserUserAgent() string {
	if ua, ok := adUserAgents[runtime.GOOS]; ok {
		return ua
	}
	return adUserAgents["linux"]
}

// waitingRoomChainTimeout bounds the whole best-effort pre-session chain so
// a hung upstream never blocks a session create for long.
const waitingRoomChainTimeout = 15 * time.Second

// FireWaitingRoomChain runs the pre-session ad flow (issue #94(b),
// WAITING_ROOM_CHAIN gate): ONE POST /api/v1/ads to the primary provider,
// then GET /api/v1/freebuff/streak — the two calls the live CLI emits per
// waiting-room episode (wire capture 2026-09-25, commit 8c96a446).
//
// The older shape — an auction per configured provider plus impression/click
// legs — was retired. The CLI never sends those legs, and the click leg had
// no user gesture behind it (the proxy renders no ad card, so nothing was
// clicked), which fabricated engagement signal the CLI only emits on a real
// click. Re-adding a leg needs a live capture proving the CLI sends it.
//
// Why "waiting_room" is the right surface for a PRE-session call: it is the
// legacy wire name for the freebuff landing screen
// (cli/src/hooks/use-gravity-ad.ts:97-99), the screen
// freebuff-landing-screen.tsx:477 mounts with enabled:true, forceStart:true
// ("this is where monetization lives"). The CLI fires that auction as the
// landing screen mounts — i.e. before a session exists — and again each time
// a 428 waiting_room_required sends it back there (endsTheSession:true;
// common/src/types/freebuff-session.ts FREEBUFF_GATE_CODES). sessionID is
// therefore normally "" and the payload omits the key; a caller passing a
// value must pass a real session instance id, never a run id.
//
// Strictly best-effort: every failure is logged and swallowed; the caller
// must never depend on it. Its value is keeping the account's pre-session ad
// engagement present before the next session create — the fork's own ban
// post-mortem (fb986b48) found that reaching admission WITHOUT that
// engagement is the shape upstream flags.
func (c *Client) FireWaitingRoomChain(ctx context.Context, sessionID string) {
	ctx, cancel := context.WithTimeout(ctx, waitingRoomChainTimeout)
	defer cancel()
	// One auction per episode, to the primary provider only: the live CLI
	// fires gravity once and moves on — there is no second-provider retry
	// inside the same wait (wire capture 2026-09-25, docs/operations/
	// bun-1.3.14-clienthello.txt companion log).
	if _, err := c.requestAds(ctx, waitingRoomAdProviders[0], sessionID); err != nil {
		slog.Debug("waiting room chain: ads request failed", "provider", waitingRoomAdProviders[0], "err", err)
	}
	if err := c.getStreak(ctx); err != nil {
		slog.Debug("waiting room chain: streak request failed", "err", err)
	}
}

// adsBaseURL is the ads origin. The host and the path are chosen together by
// use-gravity-ad.ts:528-530:
//
//	`${capabilityRoute ? FREEBUFF_WEB_URL : WEBSITE_URL}${capabilityRoute ? '/api/ads' : '/api/v1/ads'}`
//
// capabilityRoute is true only when sponsoredCliCapability() resolved, so the
// ordinary CLI auction is WEBSITE_URL + "/api/v1/ads", where WEBSITE_URL is
// NEXT_PUBLIC_CODEBUFF_APP_URL — prod https://www.codebuff.com
// (common/src/env-schema.ts:7, common/src/ads/local-agentic-test.test.ts:39).
//
// The freebuff.com/api/ads branch is a DIFFERENT endpoint with a DIFFERENT
// surface enum. It rejects surface "waiting_room" outright:
//
//	400 {"error":"Invalid request body","details":{"surface":{"_errors":
//	  ["Invalid option: expected one of \"ios\"|\"freebuff_web_chat\"|
//	    \"chat_assistant\"|\"chat_assistant_sr\"|\"cli_chat\""]}}}
//
// This fork previously posted there, so EVERY waiting-room chain 400'd and
// the pre-session ad engagement never reached upstream. Verified live
// 2026-09-25 against a real token, same payload both ways:
// www.codebuff.com/api/v1/ads + surface "waiting_room" + placementIds
// ["waiting-room-1"] -> 200 with a first-party ad; freebuff.com/api/ads ->
// 400 above. Tests point it at the mock via SetAdsBaseURLForTest.
var adsBaseURL = "https://www.codebuff.com"

// SetAdsBaseURLForTest repoints the ads origin (test-only).
func SetAdsBaseURLForTest(u string) func() {
	old := adsBaseURL
	adsBaseURL = u
	return func() { adsBaseURL = old }
}

// waitingRoomAdProviders mirrors the live CLI ad surfaces
// (use-gravity-ad.ts AdProvider: gravity | carbon | imprezia — the
// freebuff2api "zeroclick" reference provider is rejected upstream with
// "Invalid option" and was retired from the wire).
var waitingRoomAdProviders = []string{"gravity", "carbon"}

// requestAds POSTs one /api/v1/ads payload (reference cli/src/hooks/
// use-gravity-ad.ts fetchAd + common/src/util/ad-user-agent.ts: provider +
// device block + browser-like body userAgent + Freebuff-CLI header UA).
// Faithful details kept: messages stays [] and sessionId is omitted (the
// chain fires before a session exists — a fresh waiting-room).
// On success it returns the first auctioned ad's server-issued impUrl ("" when
// the auction answered with no ads), which the impression/click legs ack;
// the impUrl is never invented locally.
func (c *Client) requestAds(ctx context.Context, provider, sessionID string) (string, error) {
	payload := map[string]any{
		"provider": provider,
		"messages": []any{},
		"device": map[string]any{
			"os":       deviceOS(),
			"timezone": egressDeviceTimezone(),
			"locale":   egressDeviceLocale(),
		},
		"userAgent": adBrowserUserAgent(),
		"surface":   "waiting_room",
		// Live-capture fields (2026-09-25): the CLI carries a
		// capabilityInspection block and the placementIds array. sessionId is
		// added only when the caller has one (the live CLI captures showed a
		// session-scoped id; a fresh waiting-room omits it).
		"capabilityInspection": map[string]any{"status": "unavailable", "reason": "windows_no_containment"},
		"placementIds":         []string{"waiting-room-1"},
	}
	if sessionID != "" {
		payload["sessionId"] = sessionID
	}
	body, _ := json.Marshal(payload)
	// The ads auction is a sibling route on the ads origin, reached directly
	// rather than through newRequest (see adsBaseURL for why the host and
	// path are chosen together). newRequest would also stamp the chat
	// ai-sdk UA, which the header UA below deliberately replaces.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adsBaseURL+"/api/v1/ads", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	// Header UA: Freebuff-CLI/<version> (getCliAdRequestUserAgent), NOT the
	// chat ai-sdk UA newRequest set — the CLI's ads POST carries exactly
	// this product UA (#124).
	req.Header.Set("User-Agent", freebuffCliUA)
	resp, cancel, classErr := c.do(req, c.sessionCallTimeout)
	if classErr != nil && resp == nil {
		return "", classErr
	}
	// do() returns a nil cancel when the context already carried a deadline
	// (the chain's own timeout), so guard the defer.
	if cancel != nil {
		defer cancel()
	}
	defer func() { _ = resp.Body.Close() }()
	if classErr != nil {
		// do() classified a >=400 response once; the ads path keeps its own
		// descriptive error from the (already-read) body.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAdResponseRead))
		return "", fmt.Errorf("ads status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	// The auction body carries the ads array (fetchAd reads data.ads); parse
	// it on a wider cap than the error path — an ad payload exceeds 512B.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAdAuctionRead))
	var auction struct {
		Ads []struct {
			ImpURL string `json:"impUrl"`
		} `json:"ads"`
	}
	if err := json.Unmarshal(raw, &auction); err != nil || len(auction.Ads) == 0 {
		return "", nil
	}
	return auction.Ads[0].ImpURL, nil
}

// deviceOS maps runtime.GOOS to the ads device block's wire contract
// (macos|windows|linux, use-gravity-ad.ts getDeviceInfo platformToOs):
// Go reports "darwin" but the API only accepts "macos", and anything
// unrecognized falls back to "linux" exactly like the CLI.
func deviceOS() string {
	return deviceOSFor(runtime.GOOS)
}

func deviceOSFor(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// egressDeviceTimezone returns the host IANA timezone name for the ads
// device block, mirroring the CLI's
// Intl.DateTimeFormat().resolvedOptions().timeZone (use-gravity-ad.ts
// getDeviceInfo). time.Local.String() is the host zone when Go resolved a
// real IANA name; "Local" is Go's placeholder when it could not, so that
// (and anything LoadLocation rejects) falls back to the always-valid "UTC".
func egressDeviceTimezone() string {
	tz := time.Local.String()
	if tz == "" || tz == "Local" {
		return "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}
	return tz
}

// egressDeviceLocale returns the host locale for the ads device block,
// derived from LC_ALL/LC_MESSAGES/LANG (POSIX "en_US.UTF-8" → "en-US",
// charset stripped, "_" → "-"), falling back to "en-US" — the CLI's
// Intl.DateTimeFormat().resolvedOptions().locale shape (use-gravity-ad.ts
// getDeviceInfo). "C"/"POSIX" are not real locales and are skipped.
func egressDeviceLocale() string {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		raw := os.Getenv(env)
		if raw == "" {
			continue
		}
		lang := strings.SplitN(raw, ".", 2)[0]
		lang = strings.ReplaceAll(lang, "_", "-")
		if lang == "" || lang == "C" || lang == "POSIX" {
			continue
		}
		return lang
	}
	return "en-US"
}

// getStreak GETs /api/v1/freebuff/streak (reference
// cli/src/hooks/use-freebuff-streak-query.ts: the request() helper sets NO
// UA override → bun's default `Bun/<version>`). The proxy's equivalent of
// "no override" is newRequest's bunUserAgent (Bun/1.3.14, the pinned
// .bun-version), which is what this request inherits.
func (c *Client) getStreak(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/freebuff/streak", nil)
	if err != nil {
		return err
	}
	resp, cancel, classErr := c.do(req, c.sessionCallTimeout)
	if classErr != nil && resp == nil {
		return classErr
	}
	if cancel != nil {
		defer cancel()
	}
	defer func() { _ = resp.Body.Close() }()
	if classErr != nil {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAdResponseRead))
		return fmt.Errorf("streak status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return nil
}
