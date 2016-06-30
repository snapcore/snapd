// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// TODO: better/shorter names are probably in order once fewer legacy places are using this

const (
	// UbuntuCoreWireProtocol is the protocol level we support when
	// communicating with the store. History:
	//  - "1": client supports squashfs snaps
	UbuntuCoreWireProtocol = "1"
)

// UserAgent to send
// xxx: this should actually be set per client request, and include the client user agent
var userAgent = "unset"

func SetUserAgentFromVersion(version string) {
	extras := make([]string, 1, 3)
	extras[0] = "series " + release.Series
	if release.OnClassic {
		extras = append(extras, "classic")
	}
	if release.ReleaseInfo.ForceDevMode() {
		extras = append(extras, "devmode")
	}
	// xxx this assumes ReleaseInfo's ID and VersionID don't have weird characters
	// (see rfc 7231 for values of weird)
	// assumption checks out in practice, q.v. https://github.com/zyga/os-release-zoo
	userAgent = fmt.Sprintf("snapd/%v (%s) %s/%s (%s)", version, strings.Join(extras, "; "), release.ReleaseInfo.ID, release.ReleaseInfo.VersionID, string(arch.UbuntuArchitecture()))
}

func infoFromRemote(d snapDetails) *snap.Info {
	info := &snap.Info{}
	info.Architectures = d.Architectures
	info.Type = d.Type
	info.Version = d.Version
	info.Epoch = "0"
	info.OfficialName = d.Name
	info.SnapID = d.SnapID
	info.Revision = snap.R(d.Revision)
	info.EditedSummary = d.Summary
	info.EditedDescription = d.Description
	info.Developer = d.Developer
	info.Channel = d.Channel
	info.Sha512 = d.DownloadSha512
	info.Size = d.DownloadSize
	info.IconURL = d.IconURL
	info.AnonDownloadURL = d.AnonDownloadURL
	info.DownloadURL = d.DownloadURL
	info.Prices = d.Prices
	info.Private = d.Private
	return info
}

// SnapUbuntuStoreConfig represents the configuration to access the snap store
type SnapUbuntuStoreConfig struct {
	SearchURI     *url.URL
	BulkURI       *url.URL
	AssertionsURI *url.URL
	PurchasesURI  *url.URL
}

// SnapUbuntuStoreRepository represents the ubuntu snap store
type SnapUbuntuStoreRepository struct {
	storeID       string
	searchURI     *url.URL
	bulkURI       *url.URL
	assertionsURI *url.URL
	purchasesURI  *url.URL
	// reused http client
	client *http.Client

	mu                sync.Mutex
	suggestedCurrency string
}

func getStructFields(s interface{}) []string {
	st := reflect.TypeOf(s)
	num := st.NumField()
	fields := make([]string, 0, num)
	for i := 0; i < num; i++ {
		tag := st.Field(i).Tag.Get("json")
		idx := strings.IndexRune(tag, ',')
		if idx > -1 {
			tag = tag[:idx]
		}
		if tag != "" {
			fields = append(fields, tag)
		}
	}

	return fields
}

func cpiURL() string {
	if os.Getenv("SNAPPY_USE_STAGING_CPI") != "" {
		return "https://search.apps.staging.ubuntu.com/api/v1/"
	}
	// FIXME: this will become a store-url assertion
	if os.Getenv("SNAPPY_FORCE_CPI_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_CPI_URL")
	}

	return "https://search.apps.ubuntu.com/api/v1/"
}

func authLocation() string {
	if os.Getenv("SNAPPY_USE_STAGING_CPI") != "" {
		return "login.staging.ubuntu.com"
	}
	return "login.ubuntu.com"
}

func authURL() string {
	if os.Getenv("SNAPPY_FORCE_SSO_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_SSO_URL")
	}
	return "https://" + authLocation() + "/api/v2"
}

func assertsURL() string {
	if os.Getenv("SNAPPY_USE_STAGING_SAS") != "" {
		return "https://assertions.staging.ubuntu.com/v1/"
	}

	if os.Getenv("SNAPPY_FORCE_SAS_URL") != "" {
		return os.Getenv("SNAPPY_FORCE_SAS_URL")
	}

	return "https://assertions.ubuntu.com/v1/"
}

func myappsURL() string {
	if os.Getenv("SNAPPY_USE_STAGING_MYAPPS") != "" {
		return "https://myapps.developer.staging.ubuntu.com/"
	}
	return "https://myapps.developer.ubuntu.com/"
}

var defaultConfig = SnapUbuntuStoreConfig{}

func init() {
	storeBaseURI, err := url.Parse(cpiURL())
	if err != nil {
		panic(err)
	}

	defaultConfig.SearchURI, err = storeBaseURI.Parse("search")
	if err != nil {
		panic(err)
	}
	v := url.Values{}
	v.Set("fields", strings.Join(getStructFields(snapDetails{}), ","))
	defaultConfig.SearchURI.RawQuery = v.Encode()

	defaultConfig.BulkURI, err = storeBaseURI.Parse("metadata")
	if err != nil {
		panic(err)
	}
	defaultConfig.BulkURI.RawQuery = v.Encode()

	assertsBaseURI, err := url.Parse(assertsURL())
	if err != nil {
		panic(err)
	}

	defaultConfig.AssertionsURI, err = assertsBaseURI.Parse("assertions/")
	if err != nil {
		panic(err)
	}

	defaultConfig.PurchasesURI, err = url.Parse(myappsURL() + "dev/api/snap-purchases/")
	if err != nil {
		panic(err)
	}
}

type searchResults struct {
	Payload struct {
		Packages []snapDetails `json:"clickindex:package"`
	} `json:"_embedded"`
}

// NewUbuntuStoreSnapRepository creates a new SnapUbuntuStoreRepository with the given access configuration and for given the store id.
func NewUbuntuStoreSnapRepository(cfg *SnapUbuntuStoreConfig, storeID string) *SnapUbuntuStoreRepository {
	if cfg == nil {
		cfg = &defaultConfig
	}
	// see https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex
	return &SnapUbuntuStoreRepository{
		storeID:       storeID,
		searchURI:     cfg.SearchURI,
		bulkURI:       cfg.BulkURI,
		assertionsURI: cfg.AssertionsURI,
		purchasesURI:  cfg.PurchasesURI,
		client: &http.Client{
			Transport: &LoggedTransport{
				Transport: http.DefaultTransport,
				Key:       "SNAPD_DEBUG_HTTP",
			},
		},
	}
}

// build a new http.Request with headers for the store
func (s *SnapUbuntuStoreRepository) newRequest(method, urlStr string, body io.Reader, auther Authenticator) (*http.Request, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, err
	}

	if auther != nil {
		auther.Authenticate(req)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/hal+json")
	req.Header.Set("X-Ubuntu-Architecture", string(arch.UbuntuArchitecture()))
	req.Header.Set("X-Ubuntu-Series", release.Series)
	req.Header.Set("X-Ubuntu-Wire-Protocol", UbuntuCoreWireProtocol)

	if s.storeID != "" {
		req.Header.Set("X-Ubuntu-Store", s.storeID)
	}

	return req, nil
}

// small helper that sets the correct http headers for the ubuntu store
func (s *SnapUbuntuStoreRepository) setUbuntuStoreHeaders(req *http.Request, channel string, devmode bool, auther Authenticator) {
	if auther != nil {
		auther.Authenticate(req)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/hal+json,application/json")
	req.Header.Set("X-Ubuntu-Architecture", string(arch.UbuntuArchitecture()))
	req.Header.Set("X-Ubuntu-Release", release.Series)
	req.Header.Set("X-Ubuntu-Wire-Protocol", UbuntuCoreWireProtocol)

	if channel != "" {
		req.Header.Set("X-Ubuntu-Device-Channel", channel)
	}

	if devmode {
		req.Header.Set("X-Ubuntu-Confinement", "devmode")
	}

	if s.storeID != "" {
		req.Header.Set("X-Ubuntu-Store", s.storeID)
	}
}

// read all the available metadata from the store response and cache
func (s *SnapUbuntuStoreRepository) checkStoreResponse(resp *http.Response) {
	suggestedCurrency := resp.Header.Get("X-Suggested-Currency")

	if suggestedCurrency != "" {
		s.mu.Lock()
		s.suggestedCurrency = suggestedCurrency
		s.mu.Unlock()
	}
}

// purchase encapsulates the purchase data sent to us from the software center agent.
//
// When making a purchase request, the State "InProgress", together with a RedirectTo
// URL may be received. In-this case, the user must be directed to that webpage in
// order to complete the purchase (e.g. to enter 3D-secure credentials).
// Additionally, Partner ID may be recieved as an extended header "X-Partner-Id",
// this should be included in the follow-on requests to the redirect URL.
//
// HTTP/1.1 200 OK
// Content-Type: application/json; charset=utf-8
//
// [
//   {
//     "open_id": "https://login.staging.ubuntu.com/+id/open_id",
//     "snap_id": "8nzc1x4iim2xj1g2ul64",
//     "refundable_until": "2015-07-15 18:46:21",
//     "state": "Complete"
//   },
//   {
//     "open_id": "https://login.staging.ubuntu.com/+id/open_id",
//     "snap_id": "8nzc1x4iim2xj1g2ul64",
//     "item_sku": "item-1-sku",
//     "purchase_id": "1",
//     "refundable_until": null,
//     "state": "Complete"
//   },
//   {
//     "open_id": "https://login.staging.ubuntu.com/+id/open_id",
//     "snap_id": "12jdhg1j2dgj12dgk1jh",
//     "refundable_until": "2015-07-17 11:33:29",
//     "state": "Complete"
//   }
// ]
type purchase struct {
	OpenID          string `json:"open_id"`
	SnapID          string `json:"snap_id"`
	RefundableUntil string `json:"refundable_until"`
	State           string `json:"state"`
	ItemSKU         string `json:"item_sku,omitempty"`
	PurchaseID      string `json:"purchase_id,omitempty"`
	RedirectTo      string `json:"redirect_to,omitempty"`
}

func (s *SnapUbuntuStoreRepository) getPurchasesFromURL(url *url.URL, auther Authenticator) ([]*purchase, error) {
	if auther == nil {
		return nil, fmt.Errorf("cannot obtain known purchases from store: no authentication credentials provided")
	}

	req, err := s.newRequest("GET", url.String(), nil, auther)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var purchases []*purchase

	switch resp.StatusCode {
	case http.StatusOK:
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&purchases); err != nil {
			return nil, fmt.Errorf("cannot decode known purchases from store: %v", err)
		}
	case http.StatusUnauthorized:
		// TODO handle token expiry and refresh
		return nil, ErrInvalidCredentials
	default:
		return nil, fmt.Errorf("cannot obtain known purchases from store: server returned %v code", resp.StatusCode)
	}

	return purchases, nil
}

func setMustBuy(snaps []*snap.Info) {
	for _, info := range snaps {
		if len(info.Prices) != 0 {
			info.MustBuy = true
		}
	}
}

func hasPriced(snaps []*snap.Info) bool {
	// Search through the list of snaps to see if any are priced
	for _, info := range snaps {
		if len(info.Prices) != 0 {
			return true
		}
	}
	return false
}

// decorateAllPurchases sets the MustBuy property of each snap in the given list according to the user's known purchases.
func (s *SnapUbuntuStoreRepository) decoratePurchases(snaps []*snap.Info, auther Authenticator) error {
	// Mark every non-free snap as must buy until we know better.
	setMustBuy(snaps)

	if auther == nil {
		return nil
	}

	if !hasPriced(snaps) {
		return nil
	}

	var err error
	var purchasesURL *url.URL

	if len(snaps) == 1 {
		// If we only have a single snap, we should only find the purchases for that snap
		purchasesURL, err = s.purchasesURI.Parse(snaps[0].SnapID + "/")
		if err != nil {
			return err
		}
		q := purchasesURL.Query()
		q.Set("include_item_purchases", "true")
		purchasesURL.RawQuery = q.Encode()
	} else {
		// Inconsistently, global search implies include_item_purchases.
		purchasesURL = s.purchasesURI
	}

	purchases, err := s.getPurchasesFromURL(purchasesURL, auther)
	if err != nil {
		return err
	}

	// Group purchases by snap ID.
	purchasesByID := make(map[string][]*purchase)
	for _, purchase := range purchases {
		purchasesByID[purchase.SnapID] = append(purchasesByID[purchase.SnapID], purchase)
	}

	for _, info := range snaps {
		info.MustBuy = mustBuy(info.Prices, purchasesByID[info.SnapID])
	}

	return nil
}

// mustBuy determines if a snap requires a payment, based on if it is non-free and if the user has already bought it
func mustBuy(prices map[string]float64, purchases []*purchase) bool {
	if len(prices) == 0 {
		// If the snap is free, then it doesn't need purchasing
		return false
	}

	// Search through all the purchases for a snap to see if there are any
	// that are for the whole snap, and not an "in-app" purchase.
	for _, purchase := range purchases {
		if purchase.ItemSKU == "" {
			// Purchase is for the whole snap.
			return false
		}
	}

	// The snap is not free, and we couldn't find a purchase for the whole snap.
	return true
}

// Snap returns the snap.Info for the store hosted snap with the given name or an error.
func (s *SnapUbuntuStoreRepository) Snap(name, channel string, devmode bool, auther Authenticator) (*snap.Info, error) {
	u := *s.searchURI // make a copy, so we can mutate it

	q := u.Query()
	// exact match search
	q.Set("q", "package_name:\""+name+"\"")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	// set headers
	s.setUbuntuStoreHeaders(req, channel, devmode, auther)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// check statusCode
	switch {
	case resp.StatusCode == 404:
		return nil, ErrSnapNotFound
	case resp.StatusCode != 200:
		tpl := "Ubuntu CPI service returned unexpected HTTP status code %d while looking for snap %q in channel %q"
		if oops := resp.Header.Get("X-Oops-Id"); oops != "" {
			tpl += " [%s]"
			return nil, fmt.Errorf(tpl, resp.StatusCode, name, channel, oops)
		}
		return nil, fmt.Errorf(tpl, resp.StatusCode, name, channel)
	}

	// and decode json
	var searchData searchResults
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&searchData); err != nil {
		return nil, err
	}

	switch len(searchData.Payload.Packages) {
	case 0:
		return nil, ErrSnapNotFound
	case 1:
		// whee
	default:
		logger.Noticef("expected at most one exact match search result for %q in %q channel, got %d.", name, channel, len(searchData.Payload.Packages))
		return nil, fmt.Errorf("unexpected multiple store results for an exact match search for %q in %q channel", name, channel)
	}

	s.checkStoreResponse(resp)

	info := infoFromRemote(searchData.Payload.Packages[0])

	err = s.decoratePurchases([]*snap.Info{info}, auther)
	if err != nil {
		logger.Noticef("cannot get user purchases: %v", err)
	}

	return info, nil

}

type FindFlags int

// Find finds (installable) snaps from the store, matching the
// given search term.
func (s *SnapUbuntuStoreRepository) Find(searchTerm string, _ FindFlags, auther Authenticator) ([]*snap.Info, error) {

	u := *s.searchURI // make a copy, so we can mutate it
	q := u.Query()

	q.Set("q", searchTerm)
	u.RawQuery = q.Encode()

	req, err := s.newRequest("GET", u.String(), nil, auther)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("received an unexpected http response code (%v) when trying to search via %q", resp.Status, req.URL)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/hal+json" {
		return nil, fmt.Errorf("received an unexpected content type (%q) when trying to search via %q", ct, req.URL)
	}

	var searchData searchResults

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&searchData); err != nil {
		return nil, fmt.Errorf("cannot decode reply (got %v) when trying to search via %q", err, req.URL)
	}

	snaps := make([]*snap.Info, len(searchData.Payload.Packages))
	for i, pkg := range searchData.Payload.Packages {
		snaps[i] = infoFromRemote(pkg)
	}

	err = s.decoratePurchases(snaps, auther)
	if err != nil {
		logger.Noticef("cannot get user purchases: %v", err)
	}

	s.checkStoreResponse(resp)

	return snaps, nil
}

// RefreshCandidate contains information for the store about the currently
// installed snap so that the store can decide what update we should see
type RefreshCandidate struct {
	SnapID   string
	Revision snap.Revision
	Epoch    string
	DevMode  bool
	Block    []snap.Revision

	// the desired channel
	Channel string
}

// the exact bits that we need to send to the store
type currentSnapJson struct {
	SnapID   string `json:"snap_id"`
	Channel  string `json:"channel"`
	Revision int    `json:"revision,omitempty"`
	Epoch    string `json:"epoch"`

	// The store expects a "confinement" value {"strict", "devmode"}.
	// We map this accordingly from our devmode bool, we do not
	// use the value of the current snap as we are interested in the
	// users intention, not the actual value of the snap itself.
	Confinement snap.ConfinementType `json:"confinement"`
}

type metadataWrapper struct {
	Snaps  []currentSnapJson `json:"snaps"`
	Fields []string          `json:"fields"`
}

// ListRefresh returns the available updates for a list of snap identified by fullname with channel.
func (s *SnapUbuntuStoreRepository) ListRefresh(installed []*RefreshCandidate, auther Authenticator) (snaps []*snap.Info, err error) {

	candidateMap := map[string]*RefreshCandidate{}
	currentSnaps := make([]currentSnapJson, 0, len(installed))
	for _, cs := range installed {
		revision := cs.Revision.N
		if !cs.Revision.Store() {
			revision = 0
		}
		// the store gets confused if we send snaps without a snapid
		// (like local ones)
		if cs.SnapID == "" {
			continue
		}

		confinement := snap.StrictConfinement
		if cs.DevMode {
			confinement = snap.DevmodeConfinement
		}

		currentSnaps = append(currentSnaps, currentSnapJson{
			SnapID:      cs.SnapID,
			Channel:     cs.Channel,
			Confinement: confinement,
			Epoch:       cs.Epoch,
			Revision:    revision,
		})
		candidateMap[cs.SnapID] = cs
	}

	// build input for the updates endpoint
	jsonData, err := json.Marshal(metadataWrapper{
		Snaps: currentSnaps,
		// TODO: the store expects "origin" currently, we really want
		// it to take "developer" instead
		Fields: []string{"snap_id", "package_name", "revision", "version", "download_url", "origin"},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", s.bulkURI.String(), bytes.NewBuffer([]byte(jsonData)))
	if err != nil {
		return nil, err
	}
	// set headers
	// the updates call is a special snowflake right now
	// (see LP: #1427155)
	s.setUbuntuStoreHeaders(req, "", false, auther)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var updateData searchResults
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&updateData); err != nil {
		return nil, err
	}

	res := make([]*snap.Info, 0, len(updateData.Payload.Packages))
	for _, rsnap := range updateData.Payload.Packages {
		rrev := snap.R(rsnap.Revision)
		cand := candidateMap[rsnap.SnapID]

		// the store also gives us identical revisions, filter those
		// out, we are not interested
		if rrev == cand.Revision {
			continue
		}
		// do not upgade to a version we rolledback back from
		if findRev(rrev, cand.Block) {
			continue
		}
		res = append(res, infoFromRemote(rsnap))
	}

	s.checkStoreResponse(resp)

	return res, nil
}

func findRev(needle snap.Revision, haystack []snap.Revision) bool {
	for _, r := range haystack {
		if needle == r {
			return true
		}
	}
	return false
}

// Download downloads the snap addressed by download info and returns its
// filename.
// The file is saved in temporary storage, and should be removed
// after use to prevent the disk from running out of space.
func (s *SnapUbuntuStoreRepository) Download(name string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, auther Authenticator) (path string, err error) {
	w, err := ioutil.TempFile("", name)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(w.Name())
			path = ""
		}
	}()

	url := downloadInfo.AnonDownloadURL
	if url == "" || auther != nil {
		url = downloadInfo.DownloadURL
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	s.setUbuntuStoreHeaders(req, "", false, auther)

	if err := download(name, w, req, pbar); err != nil {
		return "", err
	}

	return w.Name(), w.Sync()
}

// download writes an http.Request showing a progress.Meter
var download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return &ErrDownload{Code: resp.StatusCode, URL: req.URL}
	}

	if pbar != nil {
		pbar.Start(name, float64(resp.ContentLength))
		mw := io.MultiWriter(w, pbar)
		_, err = io.Copy(mw, resp.Body)
		pbar.Finished()
	} else {
		_, err = io.Copy(w, resp.Body)
	}

	return err
}

type assertionSvcError struct {
	Status int    `json:"status"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Assertion retrivies the assertion for the given type and primary key.
func (s *SnapUbuntuStoreRepository) Assertion(assertType *asserts.AssertionType, primaryKey []string, auther Authenticator) (asserts.Assertion, error) {
	url, err := s.assertionsURI.Parse(path.Join(assertType.Name, path.Join(primaryKey...)))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}

	if auther != nil {
		auther.Authenticate(req)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", asserts.MediaType)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		if resp.Header.Get("Content-Type") == "application/json" {
			var svcErr assertionSvcError
			dec := json.NewDecoder(resp.Body)
			if err := dec.Decode(&svcErr); err != nil {
				return nil, fmt.Errorf("cannot decode assertion service error with HTTP status code %d: %v", resp.StatusCode, err)
			}
			if svcErr.Status == 404 {
				return nil, ErrAssertionNotFound
			}
			return nil, fmt.Errorf("assertion service error: [%s] %q", svcErr.Title, svcErr.Detail)
		}
		return nil, fmt.Errorf("unexpected HTTP status code %d", resp.StatusCode)
	}

	// and decode assertion
	dec := asserts.NewDecoder(resp.Body)
	return dec.Decode()
}

// SuggestedCurrency retrieves the cached value for the store's suggested currency
func (s *SnapUbuntuStoreRepository) SuggestedCurrency() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.suggestedCurrency == "" {
		return "USD"
	}
	return s.suggestedCurrency
}

// BuyOptions specifies parameters for store purchases.
type BuyOptions struct {
	// Required
	SnapID        string
	SnapName      string
	Channel       string
	ExpectedPrice float64
	Currency      string // ISO 4217 code as string
	Auther        Authenticator

	// Optional
	BackendID string // e.g. "credit_card", "paypal"
	MethodID  int    // e.g. a particular credit card or paypal account
}

// BuyResult holds information required to complete the purchase when state
// is "InProgress", in which case it requires user interaction to complete.
type BuyResult struct {
	State      string
	RedirectTo string
	PartnerID  string
}

// purchaseInstruction holds data sent to the store for purchases.
// X-Device-Id and X-Partner-Id (e.g. "bq") may be sent as headers.
type purchaseInstruction struct {
	SnapID    string  `json:"snap_id"`
	ItemSKU   string  `json:"item_sku,omitempty"`
	Amount    float64 `json:"amount,omitempty"`
	Currency  string  `json:"currency,omitempty"`
	BackendID string  `json:"backend_id,omitempty"`
	MethodID  int     `json:"method_id,omitempty"`
}

type buyError struct {
	ErrorMessage string `json:"error_message"`
}

func buyOptionError(options *BuyOptions, message string) (*BuyResult, error) {
	identifier := ""
	if options.SnapName != "" {
		identifier = fmt.Sprintf(" %q", options.SnapName)
	} else if options.SnapID != "" {
		identifier = fmt.Sprintf(" %q", options.SnapID)
	}

	return nil, fmt.Errorf("cannot buy snap%s: %s", identifier, message)
}

// Buy sends a purchase request for the specified snap.
// Returns the state of the purchase: Complete, Cancelled, InProgress or Pending.
func (s *SnapUbuntuStoreRepository) Buy(options *BuyOptions) (*BuyResult, error) {
	if options.SnapID == "" {
		return buyOptionError(options, "snap ID missing")
	}
	if options.SnapName == "" {
		return buyOptionError(options, "snap name missing")
	}
	if options.Channel == "" {
		return buyOptionError(options, "channel missing")
	}
	if options.ExpectedPrice <= 0 {
		return buyOptionError(options, "invalid expected price")
	}
	if options.Currency == "" {
		return buyOptionError(options, "currency missing")
	}
	if options.Auther == nil {
		return buyOptionError(options, "authentication credentials missing")
	}

	instruction := purchaseInstruction{
		SnapID:    options.SnapID,
		Amount:    options.ExpectedPrice,
		Currency:  options.Currency,
		BackendID: options.BackendID,
		MethodID:  options.MethodID,
	}

	jsonData, err := json.Marshal(instruction)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", s.purchasesURI.String(), bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	s.setUbuntuStoreHeaders(req, options.Channel, false, options.Auther)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// user already purchased or purchase successful
		var purchaseDetails purchase
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&purchaseDetails); err != nil {
			return nil, err
		}

		return &BuyResult{
			State:      purchaseDetails.State,
			RedirectTo: purchaseDetails.RedirectTo,
			PartnerID:  resp.Header.Get("X-Partner-Id"),
		}, nil
	case http.StatusBadRequest:
		// Invalid price was specified, etc.
		var errorInfo buyError
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&errorInfo); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("cannot buy snap %q: bad request: %s", options.SnapName, errorInfo.ErrorMessage)
	case http.StatusNotFound:
		// Likely because snap ID doesn't exist.
		return nil, fmt.Errorf("cannot buy snap %q: server says not found (snap got removed?)", options.SnapName)
	case http.StatusUnauthorized:
		// TODO handle token expiry and refresh
		return nil, ErrInvalidCredentials
	default:
		var errorInfo buyError
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&errorInfo); err != nil {
			return nil, err
		}
		details := ""
		if errorInfo.ErrorMessage != "" {
			details = ": " + errorInfo.ErrorMessage
		}
		return nil, fmt.Errorf("cannot buy snap %q: unexpected HTTP code %d%s", options.SnapName, resp.StatusCode, details)
	}
}
