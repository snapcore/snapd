// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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

package asserts_test

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
	"time"

	"golang.org/x/crypto/openpgp/packet"
	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

var (
	_ = Suite(&openSuite{})
	_ = Suite(&revisionErrorSuite{})
	_ = Suite(&isUnacceptedUpdateSuite{})
)

type openSuite struct{}

func (opens *openSuite) TestOpenDatabaseOK(c *C) {
	cfg := &asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))

	c.Assert(db, NotNil)
}

func (opens *openSuite) TestOpenDatabaseTrustedAccount(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"account-id":   "trusted",
		"display-name": "Trusted",
		"validation":   "verified",
		"timestamp":    "2015-01-01T14:00:00Z",
	}
	acct := mylog.Check2(asserts.AssembleAndSignInTest(asserts.AccountType, headers, nil, testPrivKey0))


	cfg := &asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   []asserts.Assertion{acct},
	}

	db := mylog.Check2(asserts.OpenDatabase(cfg))


	a := mylog.Check2(db.Find(asserts.AccountType, map[string]string{
		"account-id": "trusted",
	}))

	acct1 := a.(*asserts.Account)
	c.Check(acct1.AccountID(), Equals, "trusted")
	c.Check(acct1.DisplayName(), Equals, "Trusted")

	c.Check(db.IsTrustedAccount("trusted"), Equals, true)

	// empty account id (invalid) is not trusted
	c.Check(db.IsTrustedAccount(""), Equals, false)
}

func (opens *openSuite) TestOpenDatabaseTrustedWrongType(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "0",
	}
	a := mylog.Check2(asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey0))


	cfg := &asserts.DatabaseConfig{
		Trusted: []asserts.Assertion{a},
	}

	_ = mylog.Check2(asserts.OpenDatabase(cfg))
	c.Assert(err, ErrorMatches, "cannot predefine trusted assertions that are not account-key or account: test-only")
}

type databaseSuite struct {
	topDir string
	db     *asserts.Database
}

var _ = Suite(&databaseSuite{})

func (dbs *databaseSuite) SetUpTest(c *C) {
	dbs.topDir = filepath.Join(c.MkDir(), "asserts-db")
	fsKeypairMgr := mylog.Check2(asserts.OpenFSKeypairManager(dbs.topDir))

	cfg := &asserts.DatabaseConfig{
		KeypairManager: fsKeypairMgr,
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))

	dbs.db = db
}

func (dbs *databaseSuite) TestImportKey(c *C) {
	mylog.Check(dbs.db.ImportKey(testPrivKey1))


	keyPath := filepath.Join(dbs.topDir, "private-keys-v1", testPrivKey1SHA3_384)
	info := mylog.Check2(os.Stat(keyPath))

	c.Check(info.Mode().Perm(), Equals, os.FileMode(0600)) // secret
	// too much "clear box" testing? ok at least until we have
	// more functionality
	privKey := mylog.Check2(os.ReadFile(keyPath))


	privKeyFromDisk := mylog.Check2(asserts.DecodePrivateKeyInTest(privKey))


	c.Check(privKeyFromDisk.PublicKey().ID(), Equals, testPrivKey1SHA3_384)
}

func (dbs *databaseSuite) TestImportKeyAlreadyExists(c *C) {
	mylog.Check(dbs.db.ImportKey(testPrivKey1))

	mylog.Check(dbs.db.ImportKey(testPrivKey1))
	c.Check(err, ErrorMatches, "key pair with given key id already exists")
}

func (dbs *databaseSuite) TestPublicKey(c *C) {
	pk := testPrivKey1
	keyID := pk.PublicKey().ID()
	mylog.Check(dbs.db.ImportKey(pk))


	pubk := mylog.Check2(dbs.db.PublicKey(keyID))

	c.Check(pubk.ID(), Equals, keyID)

	// usual pattern is to then encode it
	encoded := mylog.Check2(asserts.EncodePublicKey(pubk))

	data := mylog.Check2(base64.StdEncoding.DecodeString(string(encoded)))

	c.Check(data[0], Equals, uint8(1)) // v1

	// check details of packet
	const newHeaderBits = 0x80 | 0x40
	c.Check(data[1]&newHeaderBits, Equals, uint8(newHeaderBits))
	c.Check(data[2] < 192, Equals, true) // small packet, 1 byte length
	c.Check(data[3], Equals, uint8(4))   // openpgp v4
	pkt := mylog.Check2(packet.Read(bytes.NewBuffer(data[1:])))

	pubKey, ok := pkt.(*packet.PublicKey)
	c.Assert(ok, Equals, true)
	c.Check(pubKey.PubKeyAlgo, Equals, packet.PubKeyAlgoRSA)
	c.Check(pubKey.IsSubkey, Equals, false)
	fixedTimestamp := time.Date(2016, time.January, 1, 0, 0, 0, 0, time.UTC)
	c.Check(pubKey.CreationTime.Equal(fixedTimestamp), Equals, true)
	// hash of blob content == hash of key
	h384 := sha3.Sum384(data)
	encHash := base64.RawURLEncoding.EncodeToString(h384[:])
	c.Check(encHash, DeepEquals, testPrivKey1SHA3_384)
}

func (dbs *databaseSuite) TestPublicKeyNotFound(c *C) {
	pk := testPrivKey1
	keyID := pk.PublicKey().ID()

	_ := mylog.Check2(dbs.db.PublicKey(keyID))
	c.Check(err, ErrorMatches, "cannot find key pair")
	mylog.Check(dbs.db.ImportKey(pk))


	_ = mylog.Check2(dbs.db.PublicKey("ff" + keyID))
	c.Check(err, ErrorMatches, "cannot find key pair")
}

func (dbs *databaseSuite) TestNotFoundErrorIs(c *C) {
	this := &asserts.NotFoundError{
		Headers: map[string]string{"a": "a"},
		Type:    asserts.ValidationSetType,
	}
	that := &asserts.NotFoundError{
		Headers: map[string]string{"b": "b"},
		Type:    asserts.RepairType,
	}
	c.Check(this, testutil.ErrorIs, that)
}

type checkSuite struct {
	bs asserts.Backstore
	a  asserts.Assertion
}

var _ = Suite(&checkSuite{})

func (chks *checkSuite) SetUpTest(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	chks.bs = mylog.Check2(asserts.OpenFSBackstore(topDir))


	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "0",
	}
	chks.a = mylog.Check2(asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey0))

}

func (chks *checkSuite) TestCheckNoPubKey(c *C) {
	cfg := &asserts.DatabaseConfig{
		Backstore: chks.bs,
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))

	mylog.Check(db.Check(chks.a))
	c.Assert(err, ErrorMatches, `no matching public key "[[:alnum:]_-]+" for signature by "canonical"`)
}

func (chks *checkSuite) TestCheckExpiredPubKey(c *C) {
	fixedTimeStr := "0003-01-01T00:00:00Z"
	fixedTime := mylog.Check2(time.Parse(time.RFC3339, fixedTimeStr))


	restore := asserts.MockTimeNow(fixedTime)
	defer restore()

	trustedKey := testPrivKey0

	expiredAccKey := asserts.ExpiredAccountKeyForTest("canonical", trustedKey.PublicKey())
	cfg := &asserts.DatabaseConfig{
		Backstore: chks.bs,
		Trusted:   []asserts.Assertion{expiredAccKey},
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))


	expSince := regexp.QuoteMeta(expiredAccKey.Since().Format(time.RFC3339))
	expUntil := regexp.QuoteMeta(expiredAccKey.Until().Format(time.RFC3339))
	curTime := regexp.QuoteMeta(fixedTimeStr)
	mylog.Check(db.Check(chks.a))
	c.Assert(err, ErrorMatches, fmt.Sprintf(`assertion is signed with expired public key "[[:alnum:]_-]+" from "canonical": current time is %s but key is valid during \[%s, %s\)`, curTime, expSince, expUntil))
}

func (chks *checkSuite) TestCheckExpiredPubKeyNoUntil(c *C) {
	curTimeStr := "0002-01-01T00:00:00Z"
	curTime := mylog.Check2(time.Parse(time.RFC3339, curTimeStr))


	restore := asserts.MockTimeNow(curTime)
	defer restore()

	trustedKey := testPrivKey0

	keyTimeStr := "0003-01-01T00:00:00Z"
	keyTime := mylog.Check2(time.Parse(time.RFC3339, keyTimeStr))

	expiredAccKey := asserts.MakeAccountKeyForTestWithUntil("canonical", trustedKey.PublicKey(), keyTime, time.Time{}, 1)
	cfg := &asserts.DatabaseConfig{
		Backstore: chks.bs,
		Trusted:   []asserts.Assertion{expiredAccKey},
	}

	db := mylog.Check2(asserts.OpenDatabase(cfg))

	mylog.Check(db.Check(chks.a))
	c.Assert(err, ErrorMatches, fmt.Sprintf(`assertion is signed with expired public key "[[:alnum:]_-]+" from "canonical": current time is %s but key is valid from %s`, regexp.QuoteMeta(curTimeStr), regexp.QuoteMeta(keyTimeStr)))
}

func (chks *checkSuite) TestCheckForgery(c *C) {
	trustedKey := testPrivKey0

	cfg := &asserts.DatabaseConfig{
		Backstore: chks.bs,
		Trusted:   []asserts.Assertion{asserts.BootstrapAccountKeyForTest("canonical", trustedKey.PublicKey())},
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))


	encoded := asserts.Encode(chks.a)
	content, encodedSig := chks.a.Signature()
	// forgery
	forgedSig := new(packet.Signature)
	forgedSig.PubKeyAlgo = packet.PubKeyAlgoRSA
	forgedSig.Hash = crypto.SHA512
	forgedSig.CreationTime = time.Now()
	h := crypto.SHA512.New()
	h.Write(content)
	pk1 := packet.NewRSAPrivateKey(time.Unix(1, 0), testPrivKey1RSA)
	mylog.Check(forgedSig.Sign(h, pk1, &packet.Config{DefaultHash: crypto.SHA512}))

	buf := new(bytes.Buffer)
	forgedSig.Serialize(buf)
	b := append([]byte{0x1}, buf.Bytes()...)
	forgedSigEncoded := base64.StdEncoding.EncodeToString(b)
	forgedEncoded := bytes.Replace(encoded, encodedSig, []byte(forgedSigEncoded), 1)
	c.Assert(forgedEncoded, Not(DeepEquals), encoded)

	forgedAssert := mylog.Check2(asserts.Decode(forgedEncoded))

	mylog.Check(db.Check(forgedAssert))
	c.Assert(err, ErrorMatches, "failed signature verification: .*")
}

func (chks *checkSuite) TestCheckUnsupportedFormat(c *C) {
	trustedKey := testPrivKey0

	cfg := &asserts.DatabaseConfig{
		Backstore: chks.bs,
		Trusted:   []asserts.Assertion{asserts.BootstrapAccountKeyForTest("canonical", trustedKey.PublicKey())},
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))


	var a asserts.Assertion
	(func() {
		restore := asserts.MockMaxSupportedFormat(asserts.TestOnlyType, 77)
		defer restore()

		headers := map[string]interface{}{
			"authority-id": "canonical",
			"primary-key":  "0",
			"format":       "77",
		}
		a = mylog.Check2(asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, trustedKey))

	})()
	mylog.Check(db.Check(a))
	c.Assert(err, FitsTypeOf, &asserts.UnsupportedFormatError{})
	c.Check(err, ErrorMatches, `proposed "test-only" assertion has format 77 but 1 is latest supported`)
}

func (chks *checkSuite) TestCheckMismatchedAccountIDandKey(c *C) {
	trustedKey := testPrivKey0

	cfg := &asserts.DatabaseConfig{
		Backstore: chks.bs,
		Trusted:   []asserts.Assertion{asserts.BootstrapAccountKeyForTest("canonical", trustedKey.PublicKey())},
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))


	headers := map[string]interface{}{
		"authority-id": "random",
		"primary-key":  "0",
	}
	a := mylog.Check2(asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, trustedKey))

	mylog.Check(db.Check(a))
	c.Check(err, ErrorMatches, `error finding matching public key for signature: found public key ".*" from "canonical" but expected it from: random`)
	mylog.Check(asserts.CheckSignature(a, cfg.Trusted[0].(*asserts.AccountKey), db, time.Time{}, time.Time{}))
	c.Check(err, ErrorMatches, `assertion authority "random" does not match public key from "canonical"`)
}

func (chks *checkSuite) TestCheckAndSetEarliestTime(c *C) {
	trustedKey := testPrivKey0

	ak := asserts.MakeAccountKeyForTest("canonical", trustedKey.PublicKey(), time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC), 2)

	cfg := &asserts.DatabaseConfig{
		Backstore: chks.bs,
		Trusted:   []asserts.Assertion{ak},
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))


	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "0",
	}
	a := mylog.Check2(asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, trustedKey))


	// now is since + 1 year, key is valid
	r := asserts.MockTimeNow(ak.Since().AddDate(1, 0, 0))
	defer r()
	mylog.Check(db.Check(a))
	c.Check(err, IsNil)

	// now is since - 1 year, key is invalid
	pastTime := ak.Since().AddDate(-1, 0, 0)
	asserts.MockTimeNow(pastTime)
	mylog.Check(db.Check(a))
	c.Check(err, ErrorMatches, `assertion is signed with expired public key .*`)

	// now is ignored but known to be at least >= pastTime
	// key is considered valid
	db.SetEarliestTime(pastTime)
	mylog.Check(db.Check(a))
	c.Check(err, IsNil)

	// move earliest after until
	db.SetEarliestTime(ak.Until().AddDate(0, 0, 1))
	mylog.Check(db.Check(a))
	c.Check(err, ErrorMatches, `assertion is signed with expired public key .*`)

	// check using now = since - 1 year again
	db.SetEarliestTime(time.Time{})
	mylog.Check(db.Check(a))
	c.Check(err, ErrorMatches, `assertion is signed with expired public key .*`)

	// now is since + 1 month, key is valid
	asserts.MockTimeNow(ak.Since().AddDate(0, 1, 0))
	mylog.Check(db.Check(a))
	c.Check(err, IsNil)
}

type signAddFindSuite struct {
	signingDB    *asserts.Database
	signingKeyID string
	db           *asserts.Database
}

var _ = Suite(&signAddFindSuite{})

func (safs *signAddFindSuite) SetUpTest(c *C) {
	cfg0 := &asserts.DatabaseConfig{}
	db0 := mylog.Check2(asserts.OpenDatabase(cfg0))

	safs.signingDB = db0

	pk := testPrivKey0
	mylog.Check(db0.ImportKey(pk))

	safs.signingKeyID = pk.PublicKey().ID()

	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	headers := map[string]interface{}{
		"type":         "account",
		"authority-id": "canonical",
		"account-id":   "predefined",
		"validation":   "verified",
		"display-name": "Predef",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	predefAcct := mylog.Check2(safs.signingDB.Sign(asserts.AccountType, headers, nil, safs.signingKeyID))


	trustedKey := testPrivKey0
	cfg := &asserts.DatabaseConfig{
		Backstore: bs,
		Trusted: []asserts.Assertion{
			asserts.BootstrapAccountForTest("canonical"),
			asserts.BootstrapAccountKeyForTest("canonical", trustedKey.PublicKey()),
		},
		OtherPredefined: []asserts.Assertion{
			predefAcct,
		},
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))

	safs.db = db
}

func (safs *signAddFindSuite) TestSign(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Check(a1))
	c.Check(err, IsNil)
}

func (safs *signAddFindSuite) TestSignEmptyKeyID(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, ""))
	c.Assert(err, ErrorMatches, "key id is empty")
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignMissingAuthorityId(c *C) {
	headers := map[string]interface{}{
		"primary-key": "a",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `"authority-id" header is mandatory`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignMissingPrimaryKey(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `"primary-key" header is mandatory`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignPrimaryKeyWithSlash(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "baz/9000",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `"primary-key" primary key header cannot contain '/'`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignNoPrivateKey(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, "abcd"))
	c.Assert(err, ErrorMatches, "cannot find key pair")
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignUnknownType(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(&asserts.AssertionType{Name: "xyz", PrimaryKey: nil}, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `internal error: unknown assertion type: "xyz"`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignNonPredefinedType(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(&asserts.AssertionType{Name: "test-only", PrimaryKey: nil}, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `internal error: unpredefined assertion type for name "test-only" used.*`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignBadRevision(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"revision":     "zzz",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `"revision" header is not an integer: zzz`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignBadFormat(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"format":       "zzz",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `"format" header is not an integer: zzz`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignHeadersCheck(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"extra":        []interface{}{1, 2},
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Check(err, ErrorMatches, `header "extra": header values must be strings or nested lists or maps with strings as the only scalars: 1`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignHeadersCheckMap(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"extra":        map[string]interface{}{"a": "a", "b": 1},
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Check(err, ErrorMatches, `header "extra": header values must be strings or nested lists or maps with strings as the only scalars: 1`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignAssemblerError(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"count":        "zzz",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `cannot assemble assertion test-only: "count" header is not an integer: zzz`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignUnsupportedFormat(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"format":       "77",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `cannot sign "test-only" assertion with format 77 higher than max supported format 1`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestSignInadequateFormat(c *C) {
	headers := map[string]interface{}{
		"authority-id":     "canonical",
		"primary-key":      "a",
		"format-1-feature": "true",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))
	c.Assert(err, ErrorMatches, `cannot sign "test-only" assertion with format set to 0 lower than min format 1 covering included features`)
	c.Check(a1, IsNil)
}

func (safs *signAddFindSuite) TestAddRefusesSelfSignedKey(c *C) {
	aKey := testPrivKey2

	aKeyEncoded := mylog.Check2(asserts.EncodePublicKey(aKey.PublicKey()))


	now := time.Now().UTC()
	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "canonical",
		"public-key-sha3-384": aKey.PublicKey().ID(),
		"name":                "default",
		"since":               now.Format(time.RFC3339),
	}
	acctKey := mylog.Check2(asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, aKeyEncoded, aKey))

	mylog.

		// this must fail
		Check(safs.db.Add(acctKey))
	c.Check(err, ErrorMatches, `no matching public key.*`)
}

func (safs *signAddFindSuite) TestAddSuperseding(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a1))


	retrieved1 := mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "a",
	}))

	c.Check(retrieved1, NotNil)
	c.Check(retrieved1.Revision(), Equals, 0)

	headers["revision"] = "1"
	a2 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a2))


	retrieved2 := mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "a",
	}))

	c.Check(retrieved2, NotNil)
	c.Check(retrieved2.Revision(), Equals, 1)
	mylog.Check(safs.db.Add(a1))
	c.Check(err, ErrorMatches, "revision 0 is older than current revision 1")
	c.Check(asserts.IsUnaccceptedUpdate(err), Equals, true)
}

func (safs *signAddFindSuite) TestAddNoAuthorityNoPrimaryKey(c *C) {
	headers := map[string]interface{}{
		"hdr": "FOO",
	}
	a := mylog.Check2(asserts.SignWithoutAuthority(asserts.TestOnlyNoAuthorityType, headers, nil, testPrivKey0))

	mylog.Check(safs.db.Add(a))
	c.Assert(err, ErrorMatches, `internal error: assertion type "test-only-no-authority" has no primary key`)
}

func (safs *signAddFindSuite) TestAddNoAuthorityButPrimaryKey(c *C) {
	headers := map[string]interface{}{
		"pk": "primary",
	}
	a := mylog.Check2(asserts.SignWithoutAuthority(asserts.TestOnlyNoAuthorityPKType, headers, nil, testPrivKey0))

	mylog.Check(safs.db.Add(a))
	c.Assert(err, ErrorMatches, `cannot check no-authority assertion type "test-only-no-authority-pk"`)
}

func (safs *signAddFindSuite) TestAddUnsupportedFormat(c *C) {
	const unsupported = "type: test-only\n" +
		"format: 77\n" +
		"authority-id: canonical\n" +
		"primary-key: a\n" +
		"payload: unsupported\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	aUnsupp := mylog.Check2(asserts.Decode([]byte(unsupported)))

	c.Assert(aUnsupp.SupportedFormat(), Equals, false)
	mylog.Check(safs.db.Add(aUnsupp))
	c.Assert(err, FitsTypeOf, &asserts.UnsupportedFormatError{})
	c.Check(err.(*asserts.UnsupportedFormatError).Update, Equals, false)
	c.Check(err, ErrorMatches, `proposed "test-only" assertion has format 77 but 1 is latest supported`)
	c.Check(asserts.IsUnaccceptedUpdate(err), Equals, false)

	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"format":       "1",
		"payload":      "supported",
	}
	aSupp := mylog.Check2(asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey0))

	mylog.Check(safs.db.Add(aSupp))

	mylog.Check(safs.db.Add(aUnsupp))
	c.Assert(err, FitsTypeOf, &asserts.UnsupportedFormatError{})
	c.Check(err.(*asserts.UnsupportedFormatError).Update, Equals, true)
	c.Check(err, ErrorMatches, `proposed "test-only" assertion has format 77 but 1 is latest supported \(current not updated\)`)
	c.Check(asserts.IsUnaccceptedUpdate(err), Equals, true)
}

func (safs *signAddFindSuite) TestNotFoundError(c *C) {
	err1 := &asserts.NotFoundError{
		Type: asserts.SnapDeclarationType,
		Headers: map[string]string{
			"series":  "16",
			"snap-id": "snap-id",
		},
	}
	c.Check(errors.Is(err1, &asserts.NotFoundError{}), Equals, true)
	c.Check(err1.Error(), Equals, "snap-declaration (snap-id; series:16) not found")

	err2 := &asserts.NotFoundError{
		Type: asserts.SnapRevisionType,
	}
	c.Check(errors.Is(err2, &asserts.NotFoundError{}), Equals, true)
	c.Check(err2.Error(), Equals, "snap-revision assertion not found")
}

func (safs *signAddFindSuite) TestFindNotFound(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a1))


	hdrs := map[string]string{
		"primary-key": "b",
	}
	retrieved1 := mylog.Check2(safs.db.Find(asserts.TestOnlyType, hdrs))
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.TestOnlyType,
		Headers: hdrs,
	})
	c.Check(retrieved1, IsNil)

	// checking also extra headers
	hdrs = map[string]string{
		"primary-key":  "a",
		"authority-id": "other-auth-id",
	}
	retrieved1 = mylog.Check2(safs.db.Find(asserts.TestOnlyType, hdrs))
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.TestOnlyType,
		Headers: hdrs,
	})
	c.Check(retrieved1, IsNil)
}

func (safs *signAddFindSuite) TestFindPrimaryLeftOut(c *C) {
	retrieved1 := mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{}))
	c.Assert(err, ErrorMatches, "must provide primary key: primary-key")
	c.Check(retrieved1, IsNil)
}

func (safs *signAddFindSuite) TestFindMany(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "a",
		"other":        "other-x",
	}
	aa := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(aa))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "b",
		"other":        "other-y",
	}
	ab := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(ab))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "c",
		"other":        "other-x",
	}
	ac := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(ac))


	res := mylog.Check2(safs.db.FindMany(asserts.TestOnlyType, map[string]string{
		"other": "other-x",
	}))

	c.Assert(res, HasLen, 2)
	primKeys := []string{res[0].HeaderString("primary-key"), res[1].HeaderString("primary-key")}
	sort.Strings(primKeys)
	c.Check(primKeys, DeepEquals, []string{"a", "c"})

	res = mylog.Check2(safs.db.FindMany(asserts.TestOnlyType, map[string]string{
		"other": "other-y",
	}))

	c.Assert(res, HasLen, 1)
	c.Check(res[0].Header("primary-key"), Equals, "b")

	res = mylog.Check2(safs.db.FindMany(asserts.TestOnlyType, map[string]string{}))

	c.Assert(res, HasLen, 3)

	res = mylog.Check2(safs.db.FindMany(asserts.TestOnlyType, map[string]string{
		"primary-key": "b",
		"other":       "other-y",
	}))

	c.Assert(res, HasLen, 1)

	hdrs := map[string]string{
		"primary-key": "b",
		"other":       "other-x",
	}
	res = mylog.Check2(safs.db.FindMany(asserts.TestOnlyType, hdrs))
	c.Assert(res, HasLen, 0)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.TestOnlyType,
		Headers: hdrs,
	})
}

func (safs *signAddFindSuite) TestFindFindsPredefined(c *C) {
	pk1 := testPrivKey1

	acct1 := assertstest.NewAccount(safs.signingDB, "acc-id1", map[string]interface{}{
		"authority-id": "canonical",
	}, safs.signingKeyID)

	acct1Key := assertstest.NewAccountKey(safs.signingDB, acct1, map[string]interface{}{
		"authority-id": "canonical",
	}, pk1.PublicKey(), safs.signingKeyID)
	mylog.Check(safs.db.Add(acct1))

	mylog.Check(safs.db.Add(acct1Key))


	// find the trusted key as well
	tKey := mylog.Check2(safs.db.Find(asserts.AccountKeyType, map[string]string{
		"account-id":          "canonical",
		"public-key-sha3-384": safs.signingKeyID,
	}))

	c.Assert(tKey.(*asserts.AccountKey).AccountID(), Equals, "canonical")
	c.Assert(tKey.(*asserts.AccountKey).PublicKeyID(), Equals, safs.signingKeyID)

	// find predefined account as well
	predefAcct := mylog.Check2(safs.db.Find(asserts.AccountType, map[string]string{
		"account-id": "predefined",
	}))

	c.Assert(predefAcct.(*asserts.Account).AccountID(), Equals, "predefined")
	c.Assert(predefAcct.(*asserts.Account).DisplayName(), Equals, "Predef")

	// find trusted and indirectly trusted
	accKeys := mylog.Check2(safs.db.FindMany(asserts.AccountKeyType, nil))

	c.Check(accKeys, HasLen, 2)

	accts := mylog.Check2(safs.db.FindMany(asserts.AccountType, nil))

	c.Check(accts, HasLen, 3)
}

func (safs *signAddFindSuite) TestFindTrusted(c *C) {
	pk1 := testPrivKey1

	acct1 := assertstest.NewAccount(safs.signingDB, "acc-id1", map[string]interface{}{
		"authority-id": "canonical",
	}, safs.signingKeyID)

	acct1Key := assertstest.NewAccountKey(safs.signingDB, acct1, map[string]interface{}{
		"authority-id": "canonical",
	}, pk1.PublicKey(), safs.signingKeyID)
	mylog.Check(safs.db.Add(acct1))

	mylog.Check(safs.db.Add(acct1Key))


	// find the trusted account
	tAcct := mylog.Check2(safs.db.FindTrusted(asserts.AccountType, map[string]string{
		"account-id": "canonical",
	}))

	c.Assert(tAcct.(*asserts.Account).AccountID(), Equals, "canonical")

	// find the trusted key
	tKey := mylog.Check2(safs.db.FindTrusted(asserts.AccountKeyType, map[string]string{
		"account-id":          "canonical",
		"public-key-sha3-384": safs.signingKeyID,
	}))

	c.Assert(tKey.(*asserts.AccountKey).AccountID(), Equals, "canonical")
	c.Assert(tKey.(*asserts.AccountKey).PublicKeyID(), Equals, safs.signingKeyID)

	// doesn't find not trusted assertions
	hdrs := map[string]string{
		"account-id": acct1.AccountID(),
	}
	_ = mylog.Check2(safs.db.FindTrusted(asserts.AccountType, hdrs))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.AccountType,
		Headers: hdrs,
	})

	hdrs = map[string]string{
		"account-id":          acct1.AccountID(),
		"public-key-sha3-384": acct1Key.PublicKeyID(),
	}
	_ = mylog.Check2(safs.db.FindTrusted(asserts.AccountKeyType, hdrs))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.AccountKeyType,
		Headers: hdrs,
	})

	_ = mylog.Check2(safs.db.FindTrusted(asserts.AccountType, map[string]string{
		"account-id": "predefined",
	}))
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (safs *signAddFindSuite) TestFindPredefined(c *C) {
	pk1 := testPrivKey1

	acct1 := assertstest.NewAccount(safs.signingDB, "acc-id1", map[string]interface{}{
		"authority-id": "canonical",
	}, safs.signingKeyID)

	acct1Key := assertstest.NewAccountKey(safs.signingDB, acct1, map[string]interface{}{
		"authority-id": "canonical",
	}, pk1.PublicKey(), safs.signingKeyID)
	mylog.Check(safs.db.Add(acct1))

	mylog.Check(safs.db.Add(acct1Key))


	// find the trusted account
	tAcct := mylog.Check2(safs.db.FindPredefined(asserts.AccountType, map[string]string{
		"account-id": "canonical",
	}))

	c.Assert(tAcct.(*asserts.Account).AccountID(), Equals, "canonical")

	// find the trusted key
	tKey := mylog.Check2(safs.db.FindPredefined(asserts.AccountKeyType, map[string]string{
		"account-id":          "canonical",
		"public-key-sha3-384": safs.signingKeyID,
	}))

	c.Assert(tKey.(*asserts.AccountKey).AccountID(), Equals, "canonical")
	c.Assert(tKey.(*asserts.AccountKey).PublicKeyID(), Equals, safs.signingKeyID)

	// find predefined account as well
	predefAcct := mylog.Check2(safs.db.FindPredefined(asserts.AccountType, map[string]string{
		"account-id": "predefined",
	}))

	c.Assert(predefAcct.(*asserts.Account).AccountID(), Equals, "predefined")
	c.Assert(predefAcct.(*asserts.Account).DisplayName(), Equals, "Predef")

	// doesn't find not trusted or predefined assertions
	hdrs := map[string]string{
		"account-id": acct1.AccountID(),
	}
	_ = mylog.Check2(safs.db.FindPredefined(asserts.AccountType, hdrs))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.AccountType,
		Headers: hdrs,
	})

	hdrs = map[string]string{
		"account-id":          acct1.AccountID(),
		"public-key-sha3-384": acct1Key.PublicKeyID(),
	}
	_ = mylog.Check2(safs.db.FindPredefined(asserts.AccountKeyType, hdrs))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.AccountKeyType,
		Headers: hdrs,
	})
}

func (safs *signAddFindSuite) TestFindManyPredefined(c *C) {
	headers := map[string]interface{}{
		"type":         "account",
		"authority-id": "canonical",
		"account-id":   "predefined",
		"validation":   "verified",
		"display-name": "Predef",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	predefAcct := mylog.Check2(safs.signingDB.Sign(asserts.AccountType, headers, nil, safs.signingKeyID))


	trustedKey0 := testPrivKey0
	trustedKey1 := testPrivKey1
	cfg := &asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted: []asserts.Assertion{
			asserts.BootstrapAccountForTest("canonical"),
			asserts.BootstrapAccountKeyForTest("canonical", trustedKey0.PublicKey()),
			asserts.BootstrapAccountKeyForTest("canonical", trustedKey1.PublicKey()),
		},
		OtherPredefined: []asserts.Assertion{
			predefAcct,
		},
	}
	db := mylog.Check2(asserts.OpenDatabase(cfg))


	pk1 := testPrivKey2

	acct1 := assertstest.NewAccount(safs.signingDB, "acc-id1", map[string]interface{}{
		"authority-id": "canonical",
	}, safs.signingKeyID)

	acct1Key := assertstest.NewAccountKey(safs.signingDB, acct1, map[string]interface{}{
		"authority-id": "canonical",
	}, pk1.PublicKey(), safs.signingKeyID)
	mylog.Check(db.Add(acct1))

	mylog.Check(db.Add(acct1Key))


	// find the trusted account
	tAccts := mylog.Check2(db.FindManyPredefined(asserts.AccountType, map[string]string{
		"account-id": "canonical",
	}))

	c.Assert(tAccts, HasLen, 1)
	c.Assert(tAccts[0].(*asserts.Account).AccountID(), Equals, "canonical")

	// find the predefined account
	pAccts := mylog.Check2(db.FindManyPredefined(asserts.AccountType, map[string]string{
		"account-id": "predefined",
	}))

	c.Assert(pAccts, HasLen, 1)
	c.Assert(pAccts[0].(*asserts.Account).AccountID(), Equals, "predefined")

	// find the multiple trusted keys
	tKeys := mylog.Check2(db.FindManyPredefined(asserts.AccountKeyType, map[string]string{
		"account-id": "canonical",
	}))

	c.Assert(tKeys, HasLen, 2)
	got := make(map[string]string)
	for _, a := range tKeys {
		acctKey := a.(*asserts.AccountKey)
		got[acctKey.PublicKeyID()] = acctKey.AccountID()
	}
	c.Check(got, DeepEquals, map[string]string{
		trustedKey0.PublicKey().ID(): "canonical",
		trustedKey1.PublicKey().ID(): "canonical",
	})

	// doesn't find not predefined assertions
	hdrs := map[string]string{
		"account-id": acct1.AccountID(),
	}
	_ = mylog.Check2(db.FindManyPredefined(asserts.AccountType, hdrs))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type:    asserts.AccountType,
		Headers: hdrs,
	})

	_ = mylog.Check2(db.FindManyPredefined(asserts.AccountKeyType, map[string]string{
		"account-id":          acct1.AccountID(),
		"public-key-sha3-384": acct1Key.PublicKeyID(),
	}))
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (safs *signAddFindSuite) TestDontLetAddConfusinglyAssertionClashingWithTrustedOnes(c *C) {
	// trusted
	pubKey0 := mylog.Check2(safs.signingDB.PublicKey(safs.signingKeyID))

	pubKey0Encoded := mylog.Check2(asserts.EncodePublicKey(pubKey0))


	now := time.Now().UTC()
	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "canonical",
		"public-key-sha3-384": safs.signingKeyID,
		"name":                "default",
		"since":               now.Format(time.RFC3339),
		"until":               now.AddDate(1, 0, 0).Format(time.RFC3339),
	}
	tKey := mylog.Check2(safs.signingDB.Sign(asserts.AccountKeyType, headers, []byte(pubKey0Encoded), safs.signingKeyID))

	mylog.Check(safs.db.Add(tKey))
	c.Check(err, ErrorMatches, `cannot add "account-key" assertion with primary key clashing with a trusted assertion: .*`)
}

func (safs *signAddFindSuite) TestDontLetAddConfusinglyAssertionClashingWithPredefinedOnes(c *C) {
	headers := map[string]interface{}{
		"type":         "account",
		"authority-id": "canonical",
		"account-id":   "predefined",
		"validation":   "verified",
		"display-name": "Predef",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	predefAcct := mylog.Check2(safs.signingDB.Sign(asserts.AccountType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(predefAcct))
	c.Check(err, ErrorMatches, `cannot add "account" assertion with primary key clashing with a predefined assertion: .*`)
}

func (safs *signAddFindSuite) TestFindAndRefResolve(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"pk1":          "ka",
		"pk2":          "kb",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnly2Type, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a1))


	ref := &asserts.Ref{
		Type:       asserts.TestOnly2Type,
		PrimaryKey: []string{"ka", "kb"},
	}

	resolved := mylog.Check2(ref.Resolve(safs.db.Find))

	c.Check(resolved.Headers(), DeepEquals, map[string]interface{}{
		"type":              "test-only-2",
		"authority-id":      "canonical",
		"pk1":               "ka",
		"pk2":               "kb",
		"sign-key-sha3-384": resolved.SignKeyID(),
	})

	ref = &asserts.Ref{
		Type:       asserts.TestOnly2Type,
		PrimaryKey: []string{"kb", "ka"},
	}
	_ = mylog.Check2(ref.Resolve(safs.db.Find))
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: ref.Type,
		Headers: map[string]string{
			"pk1": "kb",
			"pk2": "ka",
		},
	})
}

func (safs *signAddFindSuite) TestFindMaxFormat(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "foo",
	}
	af0 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(af0))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "foo",
		"format":       "1",
		"revision":     "1",
	}
	af1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(af1))


	a := mylog.Check2(safs.db.FindMaxFormat(asserts.TestOnlyType, map[string]string{
		"primary-key": "foo",
	}, 1))

	c.Check(a.Revision(), Equals, 1)

	a = mylog.Check2(safs.db.FindMaxFormat(asserts.TestOnlyType, map[string]string{
		"primary-key": "foo",
	}, 0))

	c.Check(a.Revision(), Equals, 0)

	a = mylog.Check2(safs.db.FindMaxFormat(asserts.TestOnlyType, map[string]string{
		"primary-key": "foo",
	}, 3))
	c.Check(err, ErrorMatches, `cannot find "test-only" assertions for format 3 higher than supported format 1`)
	c.Check(a, IsNil)
}

func (safs *signAddFindSuite) TestFindOptionalPrimaryKeys(c *C) {
	r := asserts.MockOptionalPrimaryKey(asserts.TestOnlyType, "opt1", "o1-defl")
	defer r()

	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "k1",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a1))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "k2",
		"opt1":         "A",
	}
	a2 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a2))


	a := mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "k1",
	}))

	c.Check(a.HeaderString("primary-key"), Equals, "k1")
	c.Check(a.HeaderString("opt1"), Equals, "o1-defl")

	a = mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "k1",
		"opt1":        "o1-defl",
	}))

	c.Check(a.HeaderString("primary-key"), Equals, "k1")
	c.Check(a.HeaderString("opt1"), Equals, "o1-defl")

	a = mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "k2",
		"opt1":        "A",
	}))

	c.Check(a.HeaderString("primary-key"), Equals, "k2")
	c.Check(a.HeaderString("opt1"), Equals, "A")

	_ = mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "k3",
	}))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
		Headers: map[string]string{
			"primary-key": "k3",
		},
	})

	_ = mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "k2",
	}))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
		Headers: map[string]string{
			"primary-key": "k2",
		},
	})

	_ = mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "k2",
		"opt1":        "B",
	}))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
		Headers: map[string]string{
			"primary-key": "k2",
			"opt1":        "B",
		},
	})

	_ = mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "k1",
		"opt1":        "B",
	}))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
		Headers: map[string]string{
			"primary-key": "k1",
			"opt1":        "B",
		},
	})
}

func (safs *signAddFindSuite) TestWithStackedBackstore(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "one",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a1))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "two",
	}
	a2 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))


	bs := asserts.NewMemoryBackstore()
	stacked := safs.db.WithStackedBackstore(bs)
	mylog.Check(stacked.Add(a2))


	_ = mylog.Check2(stacked.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "one",
	}))
	c.Check(err, IsNil)

	_ = mylog.Check2(stacked.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "two",
	}))
	c.Check(err, IsNil)

	_ = mylog.Check2(safs.db.Find(asserts.TestOnlyType, map[string]string{
		"primary-key": "two",
	}))
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	_ = mylog.Check2(stacked.Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": safs.signingKeyID,
	}))
	c.Check(err, IsNil)

	// stored in backstore
	_ = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"two"}, 0))
	c.Check(err, IsNil)
}

func (safs *signAddFindSuite) TestWithStackedBackstoreSafety(c *C) {
	stacked := safs.db.WithStackedBackstore(asserts.NewMemoryBackstore())

	// usual add safety
	pubKey0 := mylog.Check2(safs.signingDB.PublicKey(safs.signingKeyID))

	pubKey0Encoded := mylog.Check2(asserts.EncodePublicKey(pubKey0))


	now := time.Now().UTC()
	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "canonical",
		"public-key-sha3-384": safs.signingKeyID,
		"name":                "default",
		"since":               now.Format(time.RFC3339),
		"until":               now.AddDate(1, 0, 0).Format(time.RFC3339),
	}
	tKey := mylog.Check2(safs.signingDB.Sign(asserts.AccountKeyType, headers, []byte(pubKey0Encoded), safs.signingKeyID))

	mylog.Check(stacked.Add(tKey))
	c.Check(err, ErrorMatches, `cannot add "account-key" assertion with primary key clashing with a trusted assertion: .*`)

	// cannot go back to old revisions
	headers = map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "one",
	}
	a0 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"primary-key":  "one",
		"revision":     "1",
	}
	a1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlyType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(a1))

	mylog.Check(stacked.Add(a0))
	c.Assert(err, DeepEquals, &asserts.RevisionError{
		Used:    0,
		Current: 1,
	})
}

func (safs *signAddFindSuite) TestFindSequence(c *C) {
	headers := map[string]interface{}{
		"authority-id": "canonical",
		"n":            "s1",
		"sequence":     "1",
	}
	sq1f0 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlySeqType, headers, nil, safs.signingKeyID))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"n":            "s1",
		"sequence":     "2",
	}
	sq2f0 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlySeqType, headers, nil, safs.signingKeyID))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"format":       "1",
		"n":            "s1",
		"sequence":     "2",
		"revision":     "1",
	}
	sq2f1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlySeqType, headers, nil, safs.signingKeyID))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"format":       "1",
		"n":            "s1",
		"sequence":     "3",
	}
	sq3f1 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlySeqType, headers, nil, safs.signingKeyID))


	headers = map[string]interface{}{
		"authority-id": "canonical",
		"format":       "2",
		"n":            "s1",
		"sequence":     "3",
		"revision":     "1",
	}
	sq3f2 := mylog.Check2(safs.signingDB.Sign(asserts.TestOnlySeqType, headers, nil, safs.signingKeyID))


	for _, a := range []asserts.Assertion{sq1f0, sq2f0, sq2f1, sq3f1} {
		mylog.Check(safs.db.Add(a))

	}

	// stack a backstore, for test completeness, this is an unlikely
	// scenario atm
	bs := asserts.NewMemoryBackstore()
	db := safs.db.WithStackedBackstore(bs)
	mylog.Check(db.Add(sq3f2))


	seqHeaders := map[string]string{
		"n": "s1",
	}
	tests := []struct {
		after     int
		maxFormat int
		sequence  int
		format    int
		revision  int
	}{
		{after: 0, maxFormat: 0, sequence: 1, format: 0, revision: 0},
		{after: 0, maxFormat: 2, sequence: 1, format: 0, revision: 0},
		{after: 1, maxFormat: 0, sequence: 2, format: 0, revision: 0},
		{after: 1, maxFormat: 1, sequence: 2, format: 1, revision: 1},
		{after: 1, maxFormat: 2, sequence: 2, format: 1, revision: 1},
		{after: 2, maxFormat: 0, sequence: -1},
		{after: 2, maxFormat: 1, sequence: 3, format: 1, revision: 0},
		{after: 2, maxFormat: 2, sequence: 3, format: 2, revision: 1},
		{after: 3, maxFormat: 0, sequence: -1},
		{after: 3, maxFormat: 2, sequence: -1},
		{after: 4, maxFormat: 2, sequence: -1},
		{after: -1, maxFormat: 0, sequence: 2, format: 0, revision: 0},
		{after: -1, maxFormat: 1, sequence: 3, format: 1, revision: 0},
		{after: -1, maxFormat: 2, sequence: 3, format: 2, revision: 1},
	}

	for _, t := range tests {
		a := mylog.Check2(db.FindSequence(asserts.TestOnlySeqType, seqHeaders, t.after, t.maxFormat))
		if t.sequence == -1 {
			c.Check(err, DeepEquals, &asserts.NotFoundError{
				Type:    asserts.TestOnlySeqType,
				Headers: seqHeaders,
			})
		} else {

			c.Assert(a.HeaderString("n"), Equals, "s1")
			c.Check(a.Sequence(), Equals, t.sequence)
			c.Check(a.Format(), Equals, t.format)
			c.Check(a.Revision(), Equals, t.revision)
		}
	}

	seqHeaders = map[string]string{
		"n": "s2",
	}
	_ = mylog.Check2(db.FindSequence(asserts.TestOnlySeqType, seqHeaders, -1, 2))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlySeqType, Headers: seqHeaders,
	})
}

func (safs *signAddFindSuite) TestCheckConstraints(c *C) {
	headers := map[string]interface{}{
		"type":         "account",
		"authority-id": "canonical",
		"account-id":   "my-brand",
		"display-name": "My Brand",
		"validation":   "verified",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	acct := mylog.Check2(safs.signingDB.Sign(asserts.AccountType, headers, nil, safs.signingKeyID))

	mylog.Check(safs.db.Add(acct))
	c.Check(err, IsNil)

	pubKey1 := testPrivKey1.PublicKey()
	pubKey1Encoded := mylog.Check2(asserts.EncodePublicKey(pubKey1))


	now := time.Now().UTC()
	headers = map[string]interface{}{
		"authority-id":        "canonical",
		"format":              "1",
		"account-id":          "my-brand",
		"public-key-sha3-384": pubKey1.ID(),
		"name":                "default",
		"since":               now.Format(time.RFC3339),
		"until":               now.AddDate(1, 0, 0).Format(time.RFC3339),
		"constraints": []interface{}{
			map[string]interface{}{
				"headers": map[string]interface{}{
					"type":  "model",
					"model": "foo-.*",
				},
			},
		},
	}
	accKey := mylog.Check2(safs.signingDB.Sign(asserts.AccountKeyType, headers, []byte(pubKey1Encoded), safs.signingKeyID))

	mylog.Check(safs.db.Add(accKey))
	c.Check(err, IsNil)

	headers = map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"series":       "16",
		"model":        "foo-200",
		"classic":      "true",
		"timestamp":    now.Format(time.RFC3339),
	}
	mfoo := mylog.Check2(asserts.AssembleAndSignInTest(asserts.ModelType, headers, nil, testPrivKey1))

	mylog.Check(safs.db.Add(mfoo))
	c.Check(err, IsNil)

	headers = map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"series":       "16",
		"model":        "goo-200",
		"classic":      "true",
		"timestamp":    now.Format(time.RFC3339),
	}
	mnotfoo := mylog.Check2(asserts.AssembleAndSignInTest(asserts.ModelType, headers, nil, testPrivKey1))

	mylog.Check(safs.db.Add(mnotfoo))
	c.Check(err, ErrorMatches, `assertion does not match signing constraints for public key ".*" from "my-brand"`)
}

type revisionErrorSuite struct{}

func (res *revisionErrorSuite) TestErrorText(c *C) {
	tests := []struct {
		err      error
		expected string
	}{
		// Invalid revisions.
		{&asserts.RevisionError{Used: -1}, "assertion revision is unknown"},
		{&asserts.RevisionError{Used: -100}, "assertion revision is unknown"},
		{&asserts.RevisionError{Current: -1}, "assertion revision is unknown"},
		{&asserts.RevisionError{Current: -100}, "assertion revision is unknown"},
		{&asserts.RevisionError{Used: -1, Current: -1}, "assertion revision is unknown"},
		// Used == Current.
		{&asserts.RevisionError{}, "revision 0 is already the current revision"},
		{&asserts.RevisionError{Used: 100, Current: 100}, "revision 100 is already the current revision"},
		// Used < Current.
		{&asserts.RevisionError{Used: 1, Current: 2}, "revision 1 is older than current revision 2"},
		{&asserts.RevisionError{Used: 2, Current: 100}, "revision 2 is older than current revision 100"},
		// Used > Current.
		{&asserts.RevisionError{Current: 1, Used: 2}, "revision 2 is more recent than current revision 1"},
		{&asserts.RevisionError{Current: 2, Used: 100}, "revision 100 is more recent than current revision 2"},
	}

	for _, test := range tests {
		c.Check(test.err, ErrorMatches, test.expected)
	}
}

type isUnacceptedUpdateSuite struct{}

func (s *isUnacceptedUpdateSuite) TestIsUnacceptedUpdate(c *C) {
	tests := []struct {
		err         error
		keptCurrent bool
	}{
		{&asserts.UnsupportedFormatError{}, false},
		{&asserts.UnsupportedFormatError{Update: true}, true},
		{&asserts.RevisionError{Used: 1, Current: 1}, true},
		{&asserts.RevisionError{Used: 1, Current: 5}, true},
		{&asserts.RevisionError{Used: 3, Current: 1}, false},
		{errors.New("other error"), false},
		{&asserts.NotFoundError{Type: asserts.TestOnlyType}, false},
	}

	for _, t := range tests {
		c.Check(asserts.IsUnaccceptedUpdate(t.err), Equals, t.keptCurrent, Commentf("%v", t.err))
	}
}
