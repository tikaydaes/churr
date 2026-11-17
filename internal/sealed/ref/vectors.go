package ref

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
)

// The test vectors of docs/SEALED.md section 18: one JSON file per
// construction and record type, written from fixed inputs so the output is
// the same every time. testdata/sealed/README.md defines the format; the
// Dart, JavaScript and Swift implementations are written against those
// files alone.
//
// Every refusal in the vectors is checked here as it is written: a case
// this reference would accept never reaches a file.

// V is one JSON object of a vector file.
type V = map[string]any

// h writes bytes as lower-case hexadecimal, which is how every vector file
// carries them.
func h(b []byte) string { return hex.EncodeToString(b) }

func h32(b [32]byte) string { return hex.EncodeToString(b[:]) }

func unh(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// want panics unless a condition the vectors rely on holds.
func want(ok bool, what string) {
	if !ok {
		panic("vectors: " + what)
	}
}

// refused panics unless err is a refusal.
func refused(err error, what string) {
	if err == nil {
		panic("vectors: not refused: " + what)
	}
}

// The fixed identifiers of the vectors.
var (
	vAnn  = MustUUID("0193a000-0000-7000-8000-00000000000a")
	vBob  = MustUUID("0193a000-0000-7000-8000-00000000000b")
	vConv = MustUUID("0193a0e0-0000-7000-8000-0000000000c0")
	vCall = MustUUID("0193a0ca-0000-7000-8000-00000000ca11")
	vPass = MustUUID("0193a0aa-0000-7000-8000-0000000000f1")
)

const (
	vServer        = "chat.example.org"
	vTime   uint64 = 1800000000000
)

// vDevice is a device with fixed keys.
type vDevice struct {
	id       UUID
	account  UUID
	sign     ed25519.PrivateKey
	box      []byte
	platform byte
}

func newVDevice(name string, account UUID, id string, platform byte) vDevice {
	return vDevice{id: MustUUID(id), account: account, sign: SigningKey(Fixed("vector "+name+" sign seed", 32)),
		box: Fixed("vector "+name+" box private", 32), platform: platform}
}

func (d vDevice) entry() ListDevice {
	return ListDevice{DeviceID: d.id, SignKey: Public(d.sign), BoxKey: X25519Public(d.box), Platform: d.platform, AddedAt: vTime,
		Proof: DeviceProof(d.sign, d.account, d.id, X25519Public(d.box))}
}

func (d vDevice) json() V {
	return V{"device_id": d.id.String(), "account_id": d.account.String(), "sign_seed": h(d.sign.Seed()), "sign_key": h(Public(d.sign)),
		"box_private": h(d.box), "box_key": h(X25519Public(d.box)), "platform": d.platform}
}

var (
	vAnnPhone  = newVDevice("ann phone", vAnn, "0193aa00-0000-7000-8000-0000000000a1", PlatformAndroid)
	vAnnWeb    = newVDevice("ann web", vAnn, "0193aa00-0000-7000-8000-0000000000a2", PlatformWeb)
	vBobPhone  = newVDevice("bob phone", vBob, "0193aa00-0000-7000-8000-0000000000b1", PlatformIOS)
	vOutsider  = newVDevice("outsider", vAnn, "0193aa00-0000-7000-8000-0000000000ff", PlatformLinux)
	vAnnKey    = SigningKey(Fixed("vector ann account key seed", 32))
	vAnnKeyTwo = SigningKey(Fixed("vector ann second account key seed", 32))
	vAnnKeyNew = SigningKey(Fixed("vector ann reset account key seed", 32))
	vBobKey    = SigningKey(Fixed("vector bob account key seed", 32))
)

func listJSON(l *DeviceList) V {
	devices := []V{}
	for _, d := range l.Devices {
		devices = append(devices, V{"device_id": d.DeviceID.String(), "sign_key": h(d.SignKey), "box_key": h(d.BoxKey), "platform": d.Platform,
			"added_at": d.AddedAt, "proof": h(d.Proof)})
	}
	return V{"format": l.Format, "account_id": l.AccountID.String(), "server": l.Server, "version": l.Version, "prev": h32(l.Prev),
		"reset": l.Reset, "key_version": l.KeyVersion, "account_key": h(l.AccountKey), "devices": devices, "at": l.At}
}

// makeList builds and signs one list version.
func makeList(account UUID, version uint64, prev []byte, reset bool, keyVersion uint32, key, previous ed25519.PrivateKey, at uint64, devices ...vDevice) (*DeviceList, SignedList) {
	l := &DeviceList{Format: 1, AccountID: account, Server: vServer, Version: version, KeyVersion: keyVersion, AccountKey: Public(key), At: at}
	if prev != nil {
		l.Prev = ListHash(prev)
	}
	if reset {
		l.Reset = 1
	}
	for _, d := range devices {
		l.Devices = append(l.Devices, d.entry())
	}
	sort.Slice(l.Devices, func(i, j int) bool { return compare(l.Devices[i].DeviceID[:], l.Devices[j].DeviceID[:]) < 0 })
	raw, err := l.Encode()
	must(err)
	return l, SignList(raw, key, previous)
}

func signedJSON(s SignedList) V {
	stored, err := s.Encode()
	must(err)
	out := V{"list": h(s.List), "sig": h(s.Sig), "signed_list": h(stored), "hash": h32(ListHash(s.List))}
	if s.PrevSig != nil {
		out["prev_sig"] = h(s.PrevSig)
	}
	return out
}

func primitiveVectors() V {
	alice := unh("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	bob := unh("5dab087e624a8a4b79e17f8b83800ee66f3bb1292618b6fd1c2f8b27ff88e0eb")
	shared, err := X25519(alice, X25519Public(bob))
	must(err)
	want(h(shared) == "4a5d9d5ba4ce2de1728e3bf480350f25e07e21c947d19e3376f09b3c1e161742", "RFC 7748 shared secret")
	want(h(X25519Public(alice)) == "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a", "RFC 7748 public key")
	_, err = X25519(alice, make([]byte, 32))
	refused(err, "an all-zero X25519 result")
	ikm, salt, info := bytes.Repeat([]byte{0x0b}, 22), unh("000102030405060708090a0b0c"), unh("f0f1f2f3f4f5f6f7f8f9")
	okm := HKDF(ikm, salt, info, 42)
	want(h(okm) == "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865", "RFC 5869 test case 1")
	zero := make([]byte, 32)
	gcmEmpty, gcmBlock := gcmSeal(zero, zero[:12], nil, nil), gcmSeal(zero, zero[:12], zero[:16], nil)
	want(h(gcmEmpty) == "530f8afbc74536b9a963b4f1c4cb738b", "GCM test case 13")
	want(h(gcmBlock) == "cea7403d4d606b6e074ec5d3baf39d18d0d1c8a799996bf0265b98b5d48ab919", "GCM test case 14")
	gcmKey, gcmNonce := Fixed("vector gcm key", 32), Fixed("vector gcm nonce", 12)
	gcmAD, gcmPlain := []byte("churr file v1"), []byte("associated data is authenticated and not encrypted")
	sum := sha256.Sum256([]byte("abc"))
	want(h(sum[:]) == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "SHA-256 of abc")
	empty := sha256.Sum256(nil)
	edKey := SigningKey(unh("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"))
	edSig := ed25519.Sign(edKey, nil)
	want(h(Public(edKey)) == "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a", "RFC 8032 test 1 public key")
	want(h(edSig) == "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b", "RFC 8032 test 1 signature")
	edKey2 := SigningKey(Fixed("vector ed25519 seed", 32))
	edMsg := []byte("a message of some length to sign")
	edSig2 := ed25519.Sign(edKey2, edMsg)
	badSig := append([]byte{}, edSig2...)
	badSig[10] ^= 1
	return V{
		"description": "Published vectors for the five primitives, to prove each platform's library is being called correctly.",
		"x25519": []V{
			{"source": "RFC 7748 section 6.1", "private": h(alice), "public": h(X25519Public(bob)), "shared": h(shared)},
			{"source": "RFC 7748 section 6.1, the other way", "private": h(bob), "public": h(X25519Public(alice)), "shared": h(shared)},
			{"source": "an all-zero result is refused", "private": h(alice), "public": h(make([]byte, 32)), "refused": true},
		},
		"x25519_public": []V{
			{"source": "RFC 7748 section 6.1", "private": h(alice), "public": h(X25519Public(alice))},
			{"source": "RFC 7748 section 6.1", "private": h(bob), "public": h(X25519Public(bob))},
		},
		"hkdf_sha256": []V{
			{"source": "RFC 5869 test case 1", "ikm": h(ikm), "salt": h(salt), "info": h(info), "length": 42, "okm": h(okm)},
			{"source": "44 bytes, as seal and lock ask for", "ikm": h(Fixed("vector hkdf ikm", 32)), "salt": h(Fixed("vector hkdf salt", 16)),
				"info": h(labelled("churr lock message v1", []byte("context"))), "length": 44,
				"okm": h(HKDF(Fixed("vector hkdf ikm", 32), Fixed("vector hkdf salt", 16), labelled("churr lock message v1", []byte("context")), 44))},
			{"source": "a salt of 32 zero bytes, as the unlock keys use", "ikm": h(Fixed("vector hkdf ikm", 16)), "salt": h(zero), "info": h([]byte("churr unlock phrase v1")),
				"length": 32, "okm": h(HKDF(Fixed("vector hkdf ikm", 16), zero, []byte("churr unlock phrase v1"), 32))},
		},
		"aes_256_gcm": []V{
			{"source": "GCM specification test case 13", "key": h(zero), "nonce": h(zero[:12]), "plaintext": "", "associated_data": "", "ciphertext": h(gcmEmpty)},
			{"source": "GCM specification test case 14", "key": h(zero), "nonce": h(zero[:12]), "plaintext": h(zero[:16]), "associated_data": "", "ciphertext": h(gcmBlock)},
			{"source": "with associated data", "key": h(gcmKey), "nonce": h(gcmNonce), "plaintext": h(gcmPlain), "associated_data": h(gcmAD),
				"ciphertext": h(gcmSeal(gcmKey, gcmNonce, gcmPlain, gcmAD))},
		},
		"sha256": []V{
			{"source": "FIPS 180-4, abc", "message": h([]byte("abc")), "digest": h(sum[:])},
			{"source": "the empty message", "message": "", "digest": h(empty[:])},
		},
		"ed25519": []V{
			{"source": "RFC 8032 section 7.1, test 1", "seed": h(edKey.Seed()), "public": h(Public(edKey)), "message": "", "signature": h(edSig), "valid": true},
			{"source": "a longer message", "seed": h(edKey2.Seed()), "public": h(Public(edKey2)), "message": h(edMsg), "signature": h(edSig2), "valid": true},
			{"source": "a changed signature", "public": h(Public(edKey2)), "message": h(edMsg), "signature": h(badSig), "valid": false},
			{"source": "another message", "public": h(Public(edKey2)), "message": h([]byte("another message")), "signature": h(edSig2), "valid": false},
		},
	}
}

func encodingVectors() V {
	var u8, u32, u64, id, tm, blob, text, list Writer
	u8.U8(7)
	u32.U32(305419896)
	u64.U64(81985529216486895)
	id.UUID(vConv)
	tm.Time(vTime)
	blob.Blob([]byte{1, 2, 3})
	text.Text("héllo ✓")
	list.U32(2)
	list.UUID(vAnn)
	list.UUID(vBob)

	// The refusals, on real records.
	ur := &UnlockRequest{Format: 1, AccountID: vAnn, DeviceID: vAnnWeb.id, SignKey: Public(vAnnWeb.sign), BoxKey: X25519Public(vAnnWeb.box),
		Platform: PlatformWeb, At: vTime, Proof: DeviceProof(vAnnWeb.sign, vAnn, vAnnWeb.id, X25519Public(vAnnWeb.box))}
	good, err := ur.Encode()
	must(err)
	_, err = DecodeUnlockRequest(good)
	must(err)
	trailing := append(append([]byte{}, good...), 0)
	short := good[:len(good)-1]
	choice := append([]byte{}, good...)
	choice[1+16+16+32+32] = 9 // the platform
	format := append([]byte{}, good...)
	format[0] = 2
	l, _ := makeList(vAnn, 1, nil, false, 1, vAnnKey, nil, vTime, vAnnPhone, vAnnWeb)
	goodList, err := l.Encode()
	must(err)
	// Hand-built lists that Encode would refuse to write.
	rawList := func(server []byte, devices ...ListDevice) []byte {
		var w Writer
		w.U8(1)
		w.UUID(vAnn)
		w.Blob(server)
		w.U64(1)
		w.Raw(make([]byte, 32))
		w.U8(0)
		w.U32(1)
		w.Raw(Public(vAnnKey))
		w.U32(uint32(len(devices)))
		for _, d := range devices {
			w.UUID(d.DeviceID)
			w.Raw(d.SignKey)
			w.Raw(d.BoxKey)
			w.U8(d.Platform)
			w.Time(d.AddedAt)
			w.Raw(d.Proof)
		}
		w.Time(vTime)
		return w.b
	}
	a, b := vAnnPhone.entry(), vAnnWeb.entry()
	want(bytes.Equal(rawList([]byte(vServer), a, b), goodList), "the hand-built list matches the encoder")
	unsorted := rawList([]byte(vServer), b, a)
	repeated := rawList([]byte(vServer), a, a)
	badUTF8 := rawList([]byte{0xff, 0xfe, 0x41})
	tooLong := rawList(bytes.Repeat([]byte("a"), MaxRecord+1))
	cases := []struct {
		why, record string
		raw         []byte
		err         error
	}{
		{"bytes left over after the last field", "unlock_request", trailing, ErrTrailing},
		{"the record is cut short", "unlock_request", short, ErrShort},
		{"a choice (platform) outside its listed values", "unlock_request", choice, ErrChoice},
		{"a choice (format) outside its listed values", "unlock_request", format, ErrChoice},
		{"a list that stands for a set is not sorted by its key", "device_list", unsorted, ErrOrder},
		{"a list that stands for a set holds a key twice", "device_list", repeated, ErrOrder},
		{"a text that is not valid UTF-8", "device_list", badUTF8, ErrUTF8},
		{"a record longer than 64 KB", "device_list", tooLong, ErrTooLong},
	}
	refusals := []V{}
	for _, c := range cases {
		var err error
		if c.record == "unlock_request" {
			_, err = DecodeUnlockRequest(c.raw)
		} else {
			_, err = DecodeDeviceList(c.raw)
		}
		want(errors.Is(err, c.err), "encoding refusal: "+c.why)
		refusals = append(refusals, V{"why": c.why, "record": c.record, "bytes": h(c.raw)})
	}
	return V{
		"description": "The record encoding of SEALED.md section 2.3: each type, value to bytes, and records a reader must refuse. The fields-to-bytes vectors of each record are in that record's own file.",
		"types": []V{
			{"type": "u8", "value": "7", "bytes": h(u8.b)},
			{"type": "u32", "value": "305419896", "bytes": h(u32.b)},
			{"type": "u64", "value": "81985529216486895", "bytes": h(u64.b)},
			{"type": "uuid", "value": vConv.String(), "bytes": h(id.b)},
			{"type": "time", "value": fmt.Sprint(vTime), "note": "milliseconds since the Unix epoch, as a u64", "bytes": h(tm.b)},
			{"type": "blob", "value": "010203", "note": "the value is given in hex", "bytes": h(blob.b)},
			{"type": "text", "value": "héllo ✓", "note": "UTF-8, never normalised", "bytes": h(text.b)},
			{"type": "list of uuid", "value": []string{vAnn.String(), vBob.String()}, "bytes": h(list.b)},
		},
		"accepted": []V{
			{"record": "unlock_request", "bytes": h(good)},
			{"record": "device_list", "bytes": h(goodList)},
		},
		"refusals": refusals,
	}
}

func sealVectors() V {
	recipient := Fixed("vector seal recipient", 32)
	type in struct {
		label          string
		context, plain []byte
		oneTime        []byte
	}
	inputs := []in{
		{LabelSealEpoch, EpochSealContext(vConv, 1, vAnnPhone.id), Fixed("vector epoch secret", 32), Fixed("vector seal one-time 1", 32)},
		{LabelSealBackupKey, lockedCopyContext(vAnn, 1, WayPhrase, UUID{}), Fixed("vector backup key", 32), Fixed("vector seal one-time 2", 32)},
		{"churr seal test v1", nil, nil, Fixed("vector seal one-time 3", 32)},
		{"churr seal test v1", []byte("a context"), []byte("a plaintext that is not thirty-two bytes long"), Fixed("vector seal one-time 4", 32)},
	}
	vectors := []V{}
	var first []byte
	for i, x := range inputs {
		sealed, err := Seal(X25519Public(recipient), x.label, x.context, x.plain, x.oneTime)
		must(err)
		back, err := Open(recipient, x.label, x.context, sealed)
		must(err)
		want(bytes.Equal(back, x.plain), "seal round trip")
		if i == 0 {
			first = sealed
		}
		vectors = append(vectors, V{"recipient_private": h(recipient), "recipient_public": h(X25519Public(recipient)), "one_time_private": h(x.oneTime),
			"one_time_public": h(X25519Public(x.oneTime)), "label": x.label, "context": h(x.context), "plaintext": h(x.plain), "sealed": h(sealed)})
	}
	x := inputs[0]
	change := func(at int) []byte {
		c := append([]byte{}, first...)
		c[at] ^= 1
		return c
	}
	cases := []struct {
		why, label     string
		context, value []byte
		private        []byte
	}{
		{"a changed byte in the one-time public key", x.label, x.context, change(3), recipient},
		{"a changed byte in the ciphertext", x.label, x.context, change(40), recipient},
		{"a changed byte in the tag", x.label, x.context, change(79), recipient},
		{"a different label", LabelSealBackupKey, x.context, first, recipient},
		{"a different context", x.label, EpochSealContext(vConv, 2, vAnnPhone.id), first, recipient},
		{"another recipient's private key", x.label, x.context, first, Fixed("vector seal other recipient", 32)},
		{"an all-zero shared secret: the one-time public key is 32 zero bytes", x.label, x.context, append(make([]byte, 32), first[32:]...), recipient},
		{"cut short", x.label, x.context, first[:47], recipient},
	}
	refusals := []V{}
	for _, c := range cases {
		_, err := Open(c.private, c.label, c.context, c.value)
		refused(err, "seal: "+c.why)
		refusals = append(refusals, V{"why": c.why, "recipient_private": h(c.private), "label": c.label, "context": h(c.context), "sealed": h(c.value)})
	}
	return V{"description": "seal(R, label, context, plaintext) of SEALED.md section 2.5. one_time_private is the private half of the fresh key pair.",
		"vectors": vectors, "refusals": refusals}
}

func lockVectors() V {
	key := Fixed("vector lock key", 32)
	type in struct {
		label          string
		context, plain []byte
		salt           []byte
	}
	inputs := []in{
		{LabelLockBackupRecord, backupRecordContext(vAnn, vConv, 1, 1), Fixed("vector lock plaintext", 65), Fixed("vector lock salt 1", 16)},
		{"churr lock test v1", nil, nil, Fixed("vector lock salt 2", 16)},
		{"churr lock test v1", []byte("a context"), []byte("some plaintext"), Fixed("vector lock salt 3", 16)},
	}
	vectors := []V{}
	var first []byte
	for i, x := range inputs {
		locked := Lock(key, x.label, x.context, x.plain, x.salt)
		back, err := Unlock(key, x.label, x.context, locked)
		must(err)
		want(bytes.Equal(back, x.plain), "lock round trip")
		if i == 0 {
			first = locked
		}
		vectors = append(vectors, V{"key": h(key), "salt": h(x.salt), "label": x.label, "context": h(x.context), "plaintext": h(x.plain), "locked": h(locked)})
	}
	x := inputs[0]
	change := func(at int) []byte {
		c := append([]byte{}, first...)
		c[at] ^= 1
		return c
	}
	cases := []struct {
		why, label     string
		context, value []byte
		key            []byte
	}{
		{"a changed byte in the salt", x.label, x.context, change(2), key},
		{"a changed byte in the ciphertext", x.label, x.context, change(30), key},
		{"a changed byte in the tag", x.label, x.context, change(len(first) - 1), key},
		{"a different label", LabelLockAccountRecord, x.context, first, key},
		{"a different context", x.label, backupRecordContext(vAnn, vConv, 2, 1), first, key},
		{"another key", x.label, x.context, first, Fixed("vector lock other key", 32)},
		{"cut short", x.label, x.context, first[:31], key},
	}
	refusals := []V{}
	for _, c := range cases {
		_, err := Unlock(c.key, c.label, c.context, c.value)
		refused(err, "lock: "+c.why)
		refusals = append(refusals, V{"why": c.why, "key": h(c.key), "label": c.label, "context": h(c.context), "locked": h(c.value)})
	}
	return V{"description": "lock(K, label, context, plaintext) of SEALED.md section 2.6.", "vectors": vectors, "refusals": refusals}
}

func signatureVectors() V {
	key := SigningKey(Fixed("vector signature seed", 32))
	record := Fixed("vector signed record", 48)
	vectors := []V{}
	for _, label := range SignatureLabels {
		sig := Sign(key, label, record)
		var others []string
		for _, other := range SignatureLabels {
			if other != label {
				want(!Verify(Public(key), other, record, sig), "a signature verified under another label")
				others = append(others, other)
			}
		}
		want(Verify(Public(key), label, record, sig), "a signature verifies under its label")
		vectors = append(vectors, V{"label": label, "public": h(Public(key)), "record": h(record), "signed_bytes": h(labelled(label, record)),
			"signature": h(sig), "refused_under": others})
	}
	hashes := []V{}
	for _, label := range []string{LabelDeviceListHash, LabelEpochHash} {
		hashes = append(hashes, V{"label": label, "record": h(record), "hash": h32(Hash(label, record))})
	}
	return V{"description": "A signature is Ed25519 over label || 0x00 || record; a hash is SHA-256 of the same. Every signature here verifies under its own label and must be refused under each label in refused_under.",
		"seed": h(key.Seed()), "signatures": vectors, "hashes": hashes}
}

// vLists is the chain of Ann's device list used across the vectors, and
// Bob's one version.
type vLists struct {
	ann    []SignedList
	annP   []*DeviceList
	bob    SignedList
	bobP   *DeviceList
	steps  []string
	annKey []ed25519.PrivateKey
}

func buildLists() vLists {
	var c vLists
	add := func(l *DeviceList, s SignedList, step string, key ed25519.PrivateKey) {
		c.ann, c.annP, c.steps, c.annKey = append(c.ann, s), append(c.annP, l), append(c.steps, step), append(c.annKey, key)
	}
	l1, s1 := makeList(vAnn, 1, nil, false, 1, vAnnKey, nil, vTime, vAnnPhone)
	add(l1, s1, "first", vAnnKey)
	l2, s2 := makeList(vAnn, 2, s1.List, false, 1, vAnnKey, nil, vTime+1000, vAnnPhone, vAnnWeb)
	add(l2, s2, StepOrdinary, vAnnKey)
	l3, s3 := makeList(vAnn, 3, s2.List, false, 2, vAnnKeyTwo, vAnnKey, vTime+2000, vAnnPhone, vAnnWeb)
	add(l3, s3, StepKeyChange, vAnnKeyTwo)
	l4, s4 := makeList(vAnn, 4, s3.List, true, 3, vAnnKeyNew, nil, vTime+3000, vAnnWeb)
	add(l4, s4, StepReset, vAnnKeyNew)
	c.bobP, c.bob = makeList(vBob, 1, nil, false, 1, vBobKey, nil, vTime, vBobPhone)
	return c
}

func deviceListVectors() V {
	c := buildLists()
	chain := []V{}
	for i, s := range c.ann {
		var l *DeviceList
		var step string
		var err error
		if i == 0 {
			l, err = CheckFirstList(s)
			step = "first"
		} else {
			l, step, err = CheckListStep(c.ann[i-1].List, s)
		}
		must(err)
		want(step == c.steps[i], "the chain's step kinds")
		entry := signedJSON(s)
		entry["fields"] = listJSON(l)
		entry["step"] = step
		entry["account_key_seed"] = h(c.annKey[i].Seed())
		chain = append(chain, entry)
	}
	proofs := []V{}
	for _, d := range []vDevice{vAnnPhone, vAnnWeb, vBobPhone} {
		e := d.entry()
		proofs = append(proofs, V{"device": d.json(), "signed_record": h(ProofBytes(d.account, d.id, e.SignKey, e.BoxKey)), "label": LabelDeviceProof, "proof": h(e.Proof)})
	}
	// One refusal for each rule of section 4.4. Each is a step from
	// version 2 of the chain.
	prev := c.ann[1].List
	remake := func(change func(l *DeviceList), key, previous ed25519.PrivateKey) SignedList {
		l := &DeviceList{Format: 1, AccountID: vAnn, Server: vServer, Version: 3, Prev: ListHash(prev), KeyVersion: 1, AccountKey: Public(vAnnKey),
			At: vTime + 2000, Devices: []ListDevice{vAnnPhone.entry(), vAnnWeb.entry()}}
		change(l)
		sort.Slice(l.Devices, func(i, j int) bool { return compare(l.Devices[i].DeviceID[:], l.Devices[j].DeviceID[:]) < 0 })
		raw, err := l.Encode()
		must(err)
		return SignList(raw, key, previous)
	}
	badProof := vAnnWeb.entry()
	badProof.Proof = DeviceProof(vAnnWeb.sign, vBob, vAnnWeb.id, badProof.BoxKey) // a proof for another account
	sameKey := vOutsider.entry()
	sameKey.BoxKey = vAnnPhone.entry().BoxKey
	sameKey.Proof = DeviceProof(vOutsider.sign, vAnn, vOutsider.id, sameKey.BoxKey)
	wrongSig := remake(func(l *DeviceList) {}, vAnnKey, nil)
	wrongSig.Sig = Sign(vBobKey, LabelDeviceList, wrongSig.List)
	cases := []struct {
		rule int
		why  string
		next SignedList
	}{
		{1, "the account changed", remake(func(l *DeviceList) {
			l.AccountID = vBob
			l.Devices = []ListDevice{vBobPhone.entry()}
		}, vAnnKey, nil)},
		{1, "the server name changed", remake(func(l *DeviceList) { l.Server = "other.example.org" }, vAnnKey, nil)},
		{1, "the version is not one higher", remake(func(l *DeviceList) { l.Version = 4 }, vAnnKey, nil)},
		{1, "the version is lower than the pin", c.ann[0]},
		{1, "prev is not the hash of the previous version", remake(func(l *DeviceList) { l.Prev = ListHash(c.ann[0].List) }, vAnnKey, nil)},
		{2, "a device's proof does not verify", remake(func(l *DeviceList) { l.Devices = []ListDevice{vAnnPhone.entry(), badProof} }, vAnnKey, nil)},
		{2, "two devices have the same box key", remake(func(l *DeviceList) { l.Devices = []ListDevice{vAnnPhone.entry(), sameKey} }, vAnnKey, nil)},
		{3, "an ordinary change not signed by the account key", wrongSig},
		{3, "an ordinary change that carries a rollover signature", remake(func(l *DeviceList) {}, vAnnKey, vAnnKey)},
		{3, "the account key changed and the key version did not", remake(func(l *DeviceList) { l.AccountKey = Public(vAnnKeyTwo) }, vAnnKeyTwo, vAnnKey)},
		{3, "a key change with no signature by the previous key", remake(func(l *DeviceList) { l.AccountKey, l.KeyVersion = Public(vAnnKeyTwo), 2 }, vAnnKeyTwo, nil)},
		{3, "a key change whose rollover signature is by another key", remake(func(l *DeviceList) { l.AccountKey, l.KeyVersion = Public(vAnnKeyTwo), 2 }, vAnnKeyTwo, vBobKey)},
		{3, "the key version went up and the account key did not change", remake(func(l *DeviceList) { l.KeyVersion = 2 }, vAnnKey, nil)},
		{3, "a reset that does not raise the key version", remake(func(l *DeviceList) { l.Reset, l.AccountKey = 1, Public(vAnnKeyNew) }, vAnnKeyNew, nil)},
		{3, "a reset not signed by its own account key", func() SignedList {
			s := remake(func(l *DeviceList) { l.Reset, l.AccountKey, l.KeyVersion = 1, Public(vAnnKeyNew), 2 }, vAnnKeyNew, nil)
			s.Sig = Sign(vAnnKey, LabelDeviceList, s.List)
			return s
		}()},
	}
	refusals := []V{}
	for _, x := range cases {
		_, _, err := CheckListStep(prev, x.next)
		var le *ErrList
		want(errors.As(err, &le) && le.Rule == x.rule, fmt.Sprintf("device list refusal %q: %v", x.why, err))
		stored, err := x.next.Encode()
		must(err)
		refusals = append(refusals, V{"rule": x.rule, "why": x.why, "pinned_list": h(prev), "signed_list": h(stored)})
	}
	// First contact: only what one version allows by itself is checked.
	first := []V{}
	for _, x := range []struct {
		why  string
		next SignedList
		ok   bool
	}{
		{"any version is pinned at first contact if its own key signed it and its proofs verify", c.ann[2], true},
		{"a list not signed by its own account key", wrongSig, false},
		{"a list with a proof that does not verify", cases[5].next, false},
	} {
		_, err := CheckFirstList(x.next)
		want((err == nil) == x.ok, "first contact: "+x.why)
		stored, _ := x.next.Encode()
		first = append(first, V{"why": x.why, "signed_list": h(stored), "accepted": x.ok})
	}
	return V{
		"description": "The signed device list of SEALED.md section 4. `chain` is four versions of one account's list: the first, an ordinary change (a device added), a key change and a reset. Each refusal is a version 3 offered to a client whose pin is version 2 of the chain.",
		"account_id":  vAnn.String(), "server": vServer, "chain": chain, "proofs": proofs, "refusals": refusals, "first_contact": first,
	}
}

// vEpoch builds the epoch record used across the vectors: epoch 1 of the
// conversation, made by Ann's phone from version 2 of her list and version
// 1 of Bob's.
type vEpoch struct {
	stored  []byte
	secret  []byte
	history []byte
	lists   vLists
	oneTime map[UUID][]byte
}

func buildEpoch() vEpoch {
	e := vEpoch{secret: Fixed("vector epoch secret", 32), lists: buildLists(), oneTime: map[UUID][]byte{}}
	for _, d := range []vDevice{vAnnPhone, vAnnWeb, vBobPhone} {
		e.oneTime[d.id] = Fixed("vector epoch one-time "+d.id.String(), 32)
	}
	var err error
	e.stored, err = MakeEpoch(NewEpoch{ConversationID: vConv, KeyEpoch: 1, MakerAccountID: vAnn, MakerDeviceID: vAnnPhone.id, MakerSign: vAnnPhone.sign,
		Lists:  [2]EpochList{{AccountID: vAnn, List: e.lists.ann[1].List}, {AccountID: vBob, List: e.lists.bob.List}},
		Secret: e.secret, OneTime: func(d UUID) []byte { return e.oneTime[d] }, At: vTime + 5000})
	must(err)
	e.history = HistorySecret(e.secret, vConv, 1)
	return e
}

func (e vEpoch) listOf(account UUID, version uint64) []byte {
	if account == vAnn && version >= 1 && int(version) <= len(e.lists.ann) {
		return e.lists.ann[version-1].List
	}
	if account == vBob && version == 1 {
		return e.lists.bob.List
	}
	return nil
}

func epochJSON(stored []byte) V {
	record, sig, err := SplitStoredEpoch(stored)
	must(err)
	e, err := DecodeEpochRecord(record)
	must(err)
	accounts, recipients := []V{}, []V{}
	for _, a := range e.Accounts {
		accounts = append(accounts, V{"account_id": a.AccountID.String(), "list_version": a.ListVersion, "list_hash": h32(a.ListHash)})
	}
	for _, r := range e.Recipients {
		recipients = append(recipients, V{"account_id": r.AccountID.String(), "device_id": r.DeviceID.String(), "sealed": h(r.Sealed)})
	}
	return V{"fields": V{"format": e.Format, "conversation_id": e.ConversationID.String(), "key_epoch": e.KeyEpoch, "prev": h32(e.Prev),
		"maker_account_id": e.MakerAccountID.String(), "maker_device_id": e.MakerDeviceID.String(), "accounts": accounts, "check": h32(e.Check),
		"recipients": recipients, "at": e.At},
		"record": h(record), "signature": h(sig), "stored": h(stored), "hash": h32(EpochHash(stored))}
}

func epochVectors() V {
	e := buildEpoch()
	check := func(stored, previous []byte, d vDevice) error {
		_, err := CheckEpochRecord(EpochCheckInput{Stored: stored, ConversationID: vConv, Accounts: [2]UUID{vAnn, vBob}, Previous: previous,
			List: e.listOf, DeviceID: d.id, Box: d.box})
		return err
	}
	for _, d := range []vDevice{vAnnPhone, vAnnWeb, vBobPhone} {
		must(check(e.stored, nil, d))
	}
	content := ContentKey(e.history, vConv, 1)
	senders := V{}
	oneTime := V{}
	for _, d := range []vDevice{vAnnPhone, vAnnWeb, vBobPhone} {
		senders[d.id.String()] = h(SenderKey(content, vConv, 1, d.id))
		oneTime[d.id.String()] = h(e.oneTime[d.id])
	}
	entry := epochJSON(e.stored)
	entry["inputs"] = V{"conversation_id": vConv.String(), "key_epoch": 1, "maker": vAnnPhone.json(), "epoch_secret": h(e.secret), "one_time_private": oneTime,
		"lists": []V{{"account_id": vAnn.String(), "list": h(e.lists.ann[1].List)}, {"account_id": vBob.String(), "list": h(e.lists.bob.List)}}, "at": vTime + 5000}
	entry["derived"] = V{"salt": h(epochSalt(vConv, 1)), "history_secret": h(e.history), "content_key": h(content),
		"check": h32(EpochCheck(e.history, vConv, 1)), "sender_keys": senders,
		"seal_context": V{vAnnPhone.id.String(): h(EpochSealContext(vConv, 1, vAnnPhone.id))}}

	// A second epoch, to show the chain and give the refusals a previous
	// record.
	make2 := func(change func(n *NewEpoch)) []byte {
		n := NewEpoch{ConversationID: vConv, KeyEpoch: 2, Prev: EpochHash(e.stored), MakerAccountID: vBob, MakerDeviceID: vBobPhone.id, MakerSign: vBobPhone.sign,
			Lists:  [2]EpochList{{AccountID: vAnn, List: e.lists.ann[1].List}, {AccountID: vBob, List: e.lists.bob.List}},
			Secret: Fixed("vector second epoch secret", 32), OneTime: func(d UUID) []byte { return Fixed("vector second epoch one-time "+d.String(), 32) }, At: vTime + 9000}
		change(&n)
		stored, err := MakeEpoch(n)
		must(err)
		return stored
	}
	second := make2(func(*NewEpoch) {})
	must(check(second, e.stored, vAnnWeb))
	// reEncode rebuilds a record with changed fields and signs it again
	// with the maker's key, for refusals that are not about the signature.
	reEncode := func(stored []byte, key ed25519.PrivateKey, change func(r *EpochRecord)) []byte {
		rec, _, _ := SplitStoredEpoch(stored)
		r, err := DecodeEpochRecord(rec)
		must(err)
		change(r)
		raw, err := r.Encode()
		must(err)
		return append(raw, Sign(key, LabelEpoch, raw)...)
	}
	otherSecret := make2(func(n *NewEpoch) {})
	cases := []struct {
		rule     int
		why      string
		stored   []byte
		previous []byte
		device   vDevice
	}{
		{1, "bytes left over after the record", func() []byte {
			rec, _, _ := SplitStoredEpoch(second)
			rec = append(append([]byte{}, rec...), 0)
			return append(rec, Sign(vBobPhone.sign, LabelEpoch, rec)...)
		}(), e.stored, vAnnWeb},
		{1, "the record is for another conversation", reEncode(second, vBobPhone.sign, func(r *EpochRecord) { r.ConversationID = vCall }), e.stored, vAnnWeb},
		{2, "the key epoch is not one above the previous record's", make2(func(n *NewEpoch) { n.KeyEpoch = 3 }), e.stored, vAnnWeb},
		{2, "prev is not the hash of the previous record's stored bytes", make2(func(n *NewEpoch) { n.Prev = EpochHash(second) }), e.stored, vAnnWeb},
		{3, "the record names an account that is not in the chat", func() []byte {
			carol := MustUUID("0193a000-0000-7000-8000-00000000000c")
			return make2(func(n *NewEpoch) { n.Lists[1] = EpochList{AccountID: carol} })
		}(), e.stored, vAnnWeb},
		{4, "a list hash is not the hash of that version", reEncode(second, vBobPhone.sign, func(r *EpochRecord) { r.Accounts[0].ListHash = ListHash(e.lists.ann[0].List) }), e.stored, vAnnWeb},
		{4, "a list version lower than in the previous record", make2(func(n *NewEpoch) { n.Lists[0].List = e.lists.ann[0].List }), e.stored, vAnnWeb},
		{4, "a list version the device has not been able to check", reEncode(second, vBobPhone.sign, func(r *EpochRecord) { r.Accounts[0].ListVersion = 9 }), e.stored, vAnnWeb},
		{5, "a device of the lists is left out of the recipients", reEncode(second, vBobPhone.sign, func(r *EpochRecord) { r.Recipients = r.Recipients[1:] }), e.stored, vAnnWeb},
		{5, "a recipient is filed under the wrong account", reEncode(second, vBobPhone.sign, func(r *EpochRecord) { r.Recipients[0].AccountID = vBob }), e.stored, vAnnWeb},
		{6, "the maker is not one of the recipients", reEncode(second, vOutsider.sign, func(r *EpochRecord) { r.MakerAccountID, r.MakerDeviceID = vAnn, vOutsider.id }), e.stored, vAnnWeb},
		{6, "the signature is not by the maker's signing key", reEncode(second, vOutsider.sign, func(*EpochRecord) {}), e.stored, vAnnWeb},
		{6, "a byte of the record changed after it was signed", func() []byte {
			c := append([]byte{}, second...)
			c[len(c)-SignatureSize-1] ^= 1
			return c
		}(), e.stored, vAnnWeb},
		{7, "the device's sealed value does not open", reEncode(second, vBobPhone.sign, func(r *EpochRecord) {
			for i := range r.Recipients {
				if r.Recipients[i].DeviceID == vAnnWeb.id {
					r.Recipients[i].Sealed = append([]byte{}, r.Recipients[i].Sealed...)
					r.Recipients[i].Sealed[50] ^= 1
				}
			}
		}), e.stored, vAnnWeb},
		{7, "the secret the device opens is not the one the record's check announces", reEncode(otherSecret, vBobPhone.sign, func(r *EpochRecord) {
			r.Check = EpochCheck(Fixed("vector some other history secret", 32), vConv, 2)
		}), e.stored, vAnnWeb},
	}
	refusals := []V{}
	for _, x := range cases {
		err := check(x.stored, x.previous, x.device)
		var ee *ErrEpoch
		want(errors.As(err, &ee) && ee.Rule == x.rule, fmt.Sprintf("epoch refusal %q: %v", x.why, err))
		refusals = append(refusals, V{"rule": x.rule, "why": x.why, "stored": h(x.stored), "previous_stored": h(x.previous), "checking_device": x.device.id.String()})
	}
	// A record a device is not a recipient of is accepted by it: rule 7
	// does not apply.
	must(check(e.stored, nil, vOutsider))
	lists := []V{}
	for i, s := range e.lists.ann {
		lists = append(lists, V{"account_id": vAnn.String(), "version": i + 1, "list": h(s.List)})
	}
	lists = append(lists, V{"account_id": vBob.String(), "version": 1, "list": h(e.lists.bob.List)})
	return V{
		"description":     "The epoch record of SEALED.md section 5. `epoch_1` is built from the inputs given; `epoch_2` follows it. Each refusal is a record offered as epoch 2 to the checking device, which holds epoch_1 as the previous record and has checked the device lists in `lists_checked`.",
		"conversation_id": vConv.String(), "accounts": []string{vAnn.String(), vBob.String()},
		"devices":       []V{vAnnPhone.json(), vAnnWeb.json(), vBobPhone.json(), vOutsider.json()},
		"lists_checked": lists, "epoch_1": entry, "epoch_2": epochJSON(second), "refusals": refusals,
		"accepted_by_a_device_that_is_not_a_recipient": V{"stored": h(e.stored), "checking_device": vOutsider.id.String()},
	}
}

func messageVectors() V {
	e := buildEpoch()
	content := ContentKey(e.history, vConv, 1)
	ref1 := MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f701")
	type msg struct {
		kind    string
		content []byte
		device  vDevice
		counter uint64
		ref     UUID
	}
	file := FileEntry{ID: "0193af11-0000-7000-8000-0000000000f1", Key: B64(Fixed("vector file key", 32)), Name: "floorplan.png", MediaType: "image/png",
		Kind: "image", Size: 482113, Width: 1600, Height: 900, Caption: "Second floor",
		Thumb: &FileThumb{ID: "0193af11-0000-7000-8000-0000000000f2", Key: B64(Fixed("vector thumb key", 32)), Width: 720, Height: 405}}
	msgs := []msg{
		{KindMessage, MessageContent("Hello *Bob*", nil, nil, nil, nil), vAnnPhone, 1, ref1},
		{KindMessage, MessageContent("The plan", nil, []FileEntry{file}, &ref1, &Quote{MessageID: ref1.String()}), vBobPhone, 1, MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f702")},
		{KindEdit, EditContent(ref1, "Hello Bob", nil), vAnnPhone, 2, MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f703")},
		{KindReaction, ReactionContent(ref1, "👍", true), vBobPhone, 2, MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f704")},
		{KindTask, TaskContent(ref1, "t1", true), vAnnWeb, 1, MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f705")},
		{KindDelete, DeleteContent(ref1), vAnnPhone, 3, MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f706")},
		{KindHistory, HistoryContent([]HistorySecretEntry{{KeyEpoch: 1, Secret: B64(e.history)}}), vAnnWeb, 2, MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f707")},
	}
	sealOne := func(m msg, i int, change func(hd *Header)) ([]byte, Header, []byte) {
		hd := Header{Format: 1, ConversationID: vConv, KeyEpoch: 1, AccountID: m.device.account, DeviceID: m.device.id, Counter: m.counter, Reference: m.ref}
		if m.kind != KindMessage {
			hd.Flags = FlagQuiet
		}
		if change != nil {
			change(&hd)
		}
		salt := Fixed(fmt.Sprintf("vector message salt %d", i), 16)
		sealed, err := SealMessage(hd, SenderKey(content, vConv, 1, hd.DeviceID), salt, m.content, m.device.sign)
		must(err)
		return sealed, hd, salt
	}
	vectors := []V{}
	var sealedAll [][]byte
	for i, m := range msgs {
		sealed, hd, salt := sealOne(m, i, nil)
		gotH, got, err := OpenMessage(sealed, SenderKey(content, vConv, 1, m.device.id), Public(m.device.sign))
		must(err)
		want(bytes.Equal(got, m.content) && gotH == hd, "message round trip")
		sealedAll = append(sealedAll, sealed)
		vectors = append(vectors, V{"kind": m.kind, "quiet": hd.Quiet(),
			"header": V{"format": 1, "flags": hd.Flags, "conversation_id": vConv.String(), "key_epoch": 1, "account_id": hd.AccountID.String(),
				"device_id": hd.DeviceID.String(), "counter": hd.Counter, "reference": hd.Reference.String(), "bytes": h(hd.Encode())},
			"sender_key": h(SenderKey(content, vConv, 1, m.device.id)), "sign_key": h(Public(m.device.sign)), "salt": h(salt),
			"content": string(m.content), "content_bytes": h(m.content), "sealed": h(sealed)})
	}
	// Receiving: a device that holds epoch 1 is given rows in sequence
	// order and must come to the stated outcome for each.
	receiver := func() *Chat {
		d := &Device{Account: vBob, Server: vServer, ID: vBobPhone.id, Platform: PlatformIOS, Sign: vBobPhone.sign, Box: vBobPhone.box,
			Rand: NewStream("unused"), Now: func() uint64 { return vTime + 6000 }, Pins: map[UUID]*Pin{}, Chats: map[UUID]*Chat{}}
		must(d.LearnLists(vAnn, e.lists.ann[1:2]))
		must(d.LearnLists(vBob, []SignedList{e.lists.bob}))
		c := d.Chat(vConv, vAnn)
		must(c.AcceptEpoch(1, 1, e.stored))
		return c
	}
	c := receiver()
	changedFlag := append([]byte{}, sealedAll[0]...)
	badSig := append([]byte{}, sealedAll[0]...)
	badSig[len(badSig)-1] ^= 1
	changedCT := append([]byte{}, sealedAll[0]...)
	changedCT[HeaderSize+SaltSize+1] ^= 1
	outsider, _, _ := sealOne(msg{KindMessage, MessageContent("from a device that is not in the epoch", nil, nil, nil, nil), vOutsider, 1,
		MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f7a1")}, 20, nil)
	wrongEpoch, _, _ := sealOne(msg{KindMessage, MessageContent("in an epoch the row does not say", nil, nil, nil, nil), vAnnPhone, 9,
		MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f7a2")}, 21, func(hd *Header) { hd.KeyEpoch = 2 })
	wrongSender, _, _ := sealOne(msg{KindMessage, MessageContent("under an account the row does not say", nil, nil, nil, nil), vAnnPhone, 9,
		MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f7a3")}, 22, nil)
	late, _, _ := sealOne(msg{KindMessage, MessageContent("counter 5, delivered first", nil, nil, nil, nil), vAnnPhone, 5,
		MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f7a4")}, 23, nil)
	early, _, _ := sealOne(msg{KindMessage, MessageContent("counter 4, delivered after counter 5", nil, nil, nil, nil), vAnnPhone, 4,
		MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f7a5")}, 24, nil)
	replayedFirst, _, _ := sealOne(msg{KindMessage, MessageContent("its replay at a higher sequence number is taken in first", nil, nil, nil, nil), vAnnWeb, 1,
		MustUUID("9c051cb7-2ffa-4a41-a05d-ce900221f7a6")}, 25, nil)
	rows := []struct {
		why     string
		row     Row
		outcome string
	}{
		{"the first message", Row{Seq: 2, Author: vAnn, KeyEpoch: 1, Ciphertext: sealedAll[0]}, "shown"},
		{"an edit by the author of its target", Row{Seq: 3, Author: vAnn, KeyEpoch: 1, Quiet: true, Ciphertext: sealedAll[2]}, "shown"},
		{"a replay: the same device and counter at a second sequence number", Row{Seq: 4, Author: vAnn, KeyEpoch: 1, Ciphertext: sealedAll[0]}, "dropped"},
		{"the quiet flag of the row does not match the signed header", Row{Seq: 5, Author: vAnn, KeyEpoch: 1, Quiet: true, Ciphertext: changedFlag}, "refused"},
		{"a bad signature", Row{Seq: 6, Author: vAnn, KeyEpoch: 1, Ciphertext: badSig}, "refused"},
		{"a changed byte of the encrypted content", Row{Seq: 7, Author: vAnn, KeyEpoch: 1, Ciphertext: changedCT}, "refused"},
		{"a device that is not a recipient of the epoch", Row{Seq: 8, Author: vAnn, KeyEpoch: 1, Ciphertext: outsider}, "refused"},
		{"the header's key epoch is not the row's", Row{Seq: 9, Author: vAnn, KeyEpoch: 1, Ciphertext: wrongEpoch}, "refused"},
		{"the header's sender account is not the row's", Row{Seq: 10, Author: vBob, KeyEpoch: 1, Ciphertext: wrongSender}, "refused"},
		{"a message at a sequence number that is not after its epoch's record", Row{Seq: 1, Author: vBob, KeyEpoch: 1, Ciphertext: sealedAll[1]}, "refused"},
		{"counter 5 from a device last seen at counter 2: its counters jumped", Row{Seq: 11, Author: vAnn, KeyEpoch: 1, Ciphertext: late}, "shown_with_gap"},
		{"counter 4 after counter 5: delivered out of order", Row{Seq: 12, Author: vAnn, KeyEpoch: 1, Ciphertext: early}, "shown_out_of_order"},
		{"a replay taken in before the original (a higher sequence number read first): shown until the original arrives", Row{Seq: 14, Author: vAnn, KeyEpoch: 1, Ciphertext: replayedFirst}, "shown"},
		{"the original of that replay: it takes the replay's place, and the message is shown once", Row{Seq: 13, Author: vAnn, KeyEpoch: 1, Ciphertext: replayedFirst}, "shown"},
	}
	receive := []V{}
	for _, x := range rows {
		got, err := c.Receive(x.row)
		outcome := "refused"
		switch {
		case err != nil:
			want(errors.Is(err, ErrMessage), "a receive refusal is a failed check: "+x.why)
		case got.Dropped:
			outcome = "dropped"
		case got.OutOfOrder:
			outcome = "shown_out_of_order"
		case got.Gap:
			outcome = "shown_with_gap"
		default:
			outcome = "shown"
		}
		want(outcome == x.outcome, fmt.Sprintf("receive %q: got %s (%v), want %s", x.why, outcome, err, x.outcome))
		receive = append(receive, V{"why": x.why, "outcome": x.outcome,
			"row": V{"seq": x.row.Seq, "author_id": x.row.Author.String(), "key_epoch": x.row.KeyEpoch, "quiet": x.row.Quiet, "ciphertext": h(x.row.Ciphertext)}})
	}
	view := c.View()
	want(len(view) == 4 && view[0].Text == "Hello Bob" && view[0].Edited && view[3].Seq == 13, "the receiving device's view")
	return V{
		"description":     "The sealed message of SEALED.md section 8.1, for each kind of content, and what a receiving device makes of a run of rows (section 8.2). The epoch is epoch_1 of epoch_record.json, whose record sits at sequence number 1; the receiving device is Bob's phone.",
		"conversation_id": vConv.String(), "key_epoch": 1, "history_secret": h(e.history), "content_key": h(content),
		"messages": vectors, "receiving_device": vBobPhone.id.String(), "epoch_stored": h(e.stored), "epoch_seq": 1, "receive": receive,
		"shown_after": []V{{"reference": view[0].Reference.String(), "text": view[0].Text, "edited": true},
			{"reference": view[1].Reference.String(), "text": view[1].Text, "out_of_order": false},
			{"reference": view[2].Reference.String(), "text": view[2].Text, "out_of_order": true},
			{"reference": view[3].Reference.String(), "text": view[3].Text, "out_of_order": false}},
	}
}

// filePattern is the plaintext of the file vectors: byte i is i mod 251.
func filePattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func fileVectors() V {
	key := Fixed("vector file key", 32)
	vectors := []V{}
	for _, n := range []int{0, 1, 65535, 65536, 65537, 200000} {
		plain := filePattern(n)
		blob := EncryptFile(key, plain)
		back, err := DecryptFile(key, blob)
		must(err)
		want(bytes.Equal(back, plain), "file round trip")
		sum, psum := sha256.Sum256(blob), sha256.Sum256(plain)
		v := V{"size": n, "plaintext_sha256": h(psum[:]), "chunks": (len(blob) + sealedChunk - 1) / sealedChunk, "blob_size": len(blob), "blob_sha256": h(sum[:])}
		if n <= 1 {
			v["blob"] = h(blob)
		}
		// The first and last 32 bytes, to find a mistake without the
		// whole blob.
		v["blob_first_bytes"], v["blob_last_bytes"] = h(blob[:min(32, len(blob))]), h(blob[max(0, len(blob)-32):])
		vectors = append(vectors, v)
	}
	big := EncryptFile(key, filePattern(200000))
	swapped := append(append(append([]byte{}, big[sealedChunk:2*sealedChunk]...), big[:sealedChunk]...), big[2*sealedChunk:]...)
	short := big[:3*sealedChunk]
	one := EncryptFile(key, filePattern(65537))[:sealedChunk]
	unmarked := gcmSeal(key, chunkNonce(0, false), filePattern(1), []byte(LabelFile))
	changed := EncryptFile(key, filePattern(1))
	changed[0] ^= 1
	refusals := []V{}
	for _, x := range []struct {
		why, how string
		blob     []byte
		full     bool
	}{
		{"chunks reordered", "the blob of the 200000-byte file with its first two chunks (65552 bytes each) swapped", swapped, false},
		{"the file cut short", "the blob of the 200000-byte file with its last chunk left off (the first 196656 bytes)", short, false},
		{"the file cut short to one chunk", "the first 65552 bytes of the blob of the 65537-byte file", one, false},
		{"a missing last-chunk mark", "the 1-byte file encrypted with the last byte of the nonce 0", unmarked, true},
		{"a changed byte", "the blob of the 1-byte file with its first bit flipped", changed, true},
		{"shorter than a tag", "15 bytes", make([]byte, 15), true},
	} {
		_, err := DecryptFile(key, x.blob)
		refused(err, "file: "+x.why)
		sum := sha256.Sum256(x.blob)
		v := V{"why": x.why, "how": x.how, "blob_size": len(x.blob), "blob_sha256": h(sum[:])}
		if x.full {
			v["blob"] = h(x.blob)
		}
		refusals = append(refusals, v)
	}
	return V{
		"description": "File encryption of SEALED.md section 9. The plaintext of a file of n bytes is the bytes i mod 251 for i from 0 to n-1. Blobs larger than one byte of plaintext are given by size and SHA-256.",
		"key":         h(key), "associated_data": h([]byte(LabelFile)), "chunk_size": ChunkSize,
		"nonces": []V{{"chunk": 0, "last": false, "nonce": h(chunkNonce(0, false))}, {"chunk": 0, "last": true, "nonce": h(chunkNonce(0, true))},
			{"chunk": 3, "last": true, "nonce": h(chunkNonce(3, true))}},
		"files": vectors, "refusals": refusals,
	}
}

func backupVectors() (account, record, copies, keys, handover, unlock V) {
	backupKey := Fixed("vector backup key", 32)
	prf, credential := Fixed("vector prf output", 32), Fixed("vector credential id", 20)
	entropy := Fixed("vector phrase entropy", 16)
	passkeyPrivate, phrasePrivate := UnlockKeyFromPRF(prf, vAnn, credential), UnlockKeyFromPhrase(entropy, vAnn)
	accessRandom := Fixed("vector access key random", 32)
	accessWrap := AccessKeyWrap(accessRandom)
	accessPrivate := UnlockKeyFromAccessKey(accessWrap)

	// The account record.
	rec := &AccountRecord{Format: 1, AccountID: vAnn, KeyVersion: 1, Revision: 2, AccountKey: Public(vAnnKey), AccountKeyPrivate: vAnnKey.Seed(), At: vTime,
		Ways: []Way{{Kind: WayPasskey, ID: vPass, UnlockPublic: X25519Public(passkeyPrivate)}, {Kind: WayPhrase, UnlockPublic: X25519Public(phrasePrivate)},
			{Kind: WayAccessKey, UnlockPublic: X25519Public(accessPrivate)}}}
	content, err := rec.Encode()
	must(err)
	recSalt := Fixed("vector account record salt", 16)
	stored, err := LockAccountRecord(backupKey, rec, recSalt)
	must(err)
	back, err := OpenAccountRecord(backupKey, vAnn, 1, 2, stored)
	must(err)
	want(back.Revision == 2 && len(back.Ways) == 3, "account record round trip")
	ways := []V{}
	for _, w := range rec.Ways {
		ways = append(ways, V{"kind": w.Kind, "id": w.ID.String(), "unlock_public": h(w.UnlockPublic)})
	}
	_, e1 := OpenAccountRecord(backupKey, vAnn, 1, 3, stored)
	refused(e1, "an account record under another revision")
	_, e2 := OpenAccountRecord(backupKey, vBob, 1, 2, stored)
	refused(e2, "an account record under another account")
	_, e3 := OpenAccountRecord(Fixed("vector other backup key", 32), vAnn, 1, 2, stored)
	refused(e3, "an account record under another key")
	account = V{
		"description": "The account record of SEALED.md section 13.1: its content, and the stored form lock(backup key, \"churr lock account record v1\", account_id || key_version || revision, content).",
		"backup_key":  h(backupKey), "salt": h(recSalt),
		"fields": V{"format": 1, "account_id": vAnn.String(), "key_version": 1, "revision": 2, "account_key": h(rec.AccountKey),
			"account_key_private": h(rec.AccountKeyPrivate), "ways": ways, "at": vTime},
		"content": h(content), "context": h(accountRecordContext(vAnn, 1, 2)), "stored": h(stored),
		"refusals": []V{
			{"why": "opened as another revision", "backup_key": h(backupKey), "account_id": vAnn.String(), "key_version": 1, "revision": 3, "stored": h(stored)},
			{"why": "opened as another account's", "backup_key": h(backupKey), "account_id": vBob.String(), "key_version": 1, "revision": 2, "stored": h(stored)},
			{"why": "opened with another key", "backup_key": h(Fixed("vector other backup key", 32)), "account_id": vAnn.String(), "key_version": 1, "revision": 2, "stored": h(stored)},
		},
	}

	// A backup record.
	epoch := buildEpoch()
	br := &BackupRecord{Format: 1, ConversationID: vConv, KeyEpoch: 1, Secret: epoch.history, At: vTime}
	brContent, err := br.Encode()
	must(err)
	brSalt := Fixed("vector backup record salt", 16)
	brStored, err := LockBackupRecord(backupKey, vAnn, 1, br, brSalt)
	must(err)
	want(len(brStored) == 97, "a backup record is 97 bytes")
	got, err := OpenBackupRecord(backupKey, vAnn, vConv, 1, 1, brStored)
	must(err)
	parsedEpoch, _ := DecodeEpochRecord(epoch.stored[:len(epoch.stored)-SignatureSize])
	want(CheckHistorySecret(got.Secret, parsedEpoch), "the backup record's secret matches the epoch's check")
	type brCase struct {
		why        string
		account    UUID
		conv       UUID
		keyEpoch   uint64
		keyVersion uint32
	}
	brRefusals := []V{}
	for _, x := range []brCase{
		{"moved to another chat", vAnn, vCall, 1, 1},
		{"moved to another key epoch", vAnn, vConv, 2, 1},
		{"moved to another account", vBob, vConv, 1, 1},
		{"filed under another key version", vAnn, vConv, 1, 2},
	} {
		_, err := OpenBackupRecord(backupKey, x.account, x.conv, x.keyEpoch, x.keyVersion, brStored)
		refused(err, "backup record: "+x.why)
		brRefusals = append(brRefusals, V{"why": x.why, "account_id": x.account.String(), "conversation_id": x.conv.String(), "key_epoch": x.keyEpoch,
			"key_version": x.keyVersion, "stored": h(brStored)})
	}
	wrong := &BackupRecord{Format: 1, ConversationID: vConv, KeyEpoch: 1, Secret: Fixed("vector a made-up history secret", 32), At: vTime}
	wrongStored, _ := LockBackupRecord(backupKey, vAnn, 1, wrong, Fixed("vector backup record salt 2", 16))
	gotWrong, err := OpenBackupRecord(backupKey, vAnn, vConv, 1, 1, wrongStored)
	must(err)
	want(!CheckHistorySecret(gotWrong.Secret, parsedEpoch), "a made-up secret does not match the check")
	record = V{
		"description": "A backup record of SEALED.md section 13.2: its 65 bytes of content, and the stored form lock(backup key, \"churr lock backup record v1\", account_id || conversation_id || key_epoch || key_version, content), 97 bytes.",
		"backup_key":  h(backupKey), "salt": h(brSalt), "account_id": vAnn.String(), "key_version": 1,
		"fields":  V{"format": 1, "conversation_id": vConv.String(), "key_epoch": 1, "secret": h(epoch.history), "at": vTime},
		"content": h(brContent), "context": h(backupRecordContext(vAnn, vConv, 1, 1)), "stored": h(brStored),
		"epoch_check": h32(parsedEpoch.Check), "refusals": brRefusals,
		"opens_but_fails_the_epoch_check": V{"stored": h(wrongStored), "secret": h(wrong.Secret)},
	}

	// A locked copy for each of the three ways.
	copyList := []V{}
	type way struct {
		name    string
		kind    byte
		id      UUID
		private []byte
		oneTime []byte
	}
	var firstCopy []byte
	for i, w := range []way{
		{"passkey", WayPasskey, vPass, passkeyPrivate, Fixed("vector copy one-time passkey", 32)},
		{"phrase", WayPhrase, UUID{}, phrasePrivate, Fixed("vector copy one-time phrase", 32)},
		{"device", WayDevice, vAnnWeb.id, vAnnWeb.box, Fixed("vector copy one-time device", 32)},
		{"access_key", WayAccessKey, UUID{}, accessPrivate, Fixed("vector copy one-time access key", 32)},
	} {
		cp, err := MakeLockedCopy(X25519Public(w.private), vAnn, 1, w.kind, w.id, backupKey, vAnnKey, w.oneTime)
		must(err)
		want(len(cp) == LockedCopySize, "a locked copy is 144 bytes")
		key, err := OpenLockedCopy(w.private, vAnn, 1, w.kind, w.id, cp)
		must(err)
		want(bytes.Equal(key, backupKey) && VerifyLockedCopy(Public(vAnnKey), vAnn, 1, w.kind, w.id, cp), "locked copy round trip")
		if i == 0 {
			firstCopy = cp
		}
		copyList = append(copyList, V{"way": w.name, "kind": w.kind, "id": w.id.String(), "way_tag": h(WayTag(w.kind, w.id)), "unlock_private": h(w.private),
			"unlock_public": h(X25519Public(w.private)), "one_time_private": h(w.oneTime), "context": h(lockedCopyContext(vAnn, 1, w.kind, w.id)),
			"sealed": h(cp[:80]), "signature": h(cp[80:]), "signed_record": h(append(lockedCopyContext(vAnn, 1, w.kind, w.id), cp[:80]...)), "stored": h(cp)})
	}
	// A copy the server made itself: it opens, but the account key did
	// not sign it.
	forged, err := MakeLockedCopy(X25519Public(phrasePrivate), vAnn, 1, WayPhrase, UUID{}, Fixed("vector the server's own key", 32), vBobKey, Fixed("vector forged one-time", 32))
	must(err)
	_, err = OpenLockedCopy(phrasePrivate, vAnn, 1, WayPhrase, UUID{}, forged)
	must(err)
	want(!VerifyLockedCopy(Public(vAnnKey), vAnn, 1, WayPhrase, UUID{}, forged), "a forged copy fails the signature check")
	_, e4 := OpenLockedCopy(passkeyPrivate, vAnn, 2, WayPasskey, vPass, firstCopy)
	refused(e4, "a locked copy under another key version")
	_, e5 := OpenLockedCopy(passkeyPrivate, vAnn, 1, WayDevice, vPass, firstCopy)
	refused(e5, "a locked copy under another way")
	copies = V{
		"description": "Locked copies of SEALED.md section 13.3: the backup key sealed to an unlock public key and signed by the account key, 144 bytes, for each of the four ways.",
		"account_id":  vAnn.String(), "key_version": 1, "backup_key": h(backupKey), "account_key_seed": h(vAnnKey.Seed()), "account_key": h(Public(vAnnKey)),
		"copies": copyList,
		"refusals": []V{
			{"why": "opened as a copy for another key version", "unlock_private": h(passkeyPrivate), "kind": WayPasskey, "id": vPass.String(), "key_version": 2, "stored": h(firstCopy)},
			{"why": "opened as a copy for another way", "unlock_private": h(passkeyPrivate), "kind": WayDevice, "id": vPass.String(), "key_version": 1, "stored": h(firstCopy)},
		},
		"opens_but_is_not_signed_by_the_account_key": V{"unlock_private": h(phrasePrivate), "kind": WayPhrase, "id": UUID{}.String(), "key_version": 1,
			"stored": h(forged), "account_key": h(Public(vAnnKey))},
	}

	// Unlock keys: from a PRF output, and from the phrase.
	phrase := Phrase(entropy)
	typed, err := ReadPhrase(phrase)
	must(err)
	want(bytes.Equal(typed, entropy), "phrase round trip")
	phrases := []V{}
	for _, x := range []struct{ entropy, words string }{
		{"00000000000000000000000000000000", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"},
		{"7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f", "legal winner thank year wave sausage worth useful legal winner thank yellow"},
		{"80808080808080808080808080808080", "letter advice cage absurd amount doctor acoustic avoid letter advice cage above"},
		{"ffffffffffffffffffffffffffffffff", "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo wrong"},
		{h(entropy), phrase},
	} {
		want(Phrase(unh(x.entropy)) == x.words, "BIP-39 words")
		phrases = append(phrases, V{"entropy": x.entropy, "words": x.words})
	}
	reading := []V{}
	for _, x := range []struct {
		typed, why string
		ok         bool
	}{
		{"  LEGAL Winner thank year wave sausage worth useful legal winner thank yellow ", "case and extra spaces do not matter", true},
		{"lega winn than year wave saus wort usef lega winn than yell", "the first four letters identify a word", true},
		{"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon", "a bad checksum", false},
		{"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon", "eleven words", false},
		{"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon churr", "a word that is not on the list", false},
	} {
		got, err := ReadPhrase(x.typed)
		want((err == nil) == x.ok, "reading a phrase: "+x.why)
		v := V{"typed": x.typed, "why": x.why, "accepted": x.ok}
		if x.ok {
			v["entropy"] = h(got)
		}
		reading = append(reading, v)
	}
	keys = V{
		"description": "Unlock keys of SEALED.md section 13.3: an X25519 key pair derived from a passkey's PRF output, one derived from the 16 bytes the twelve words stand for, and one derived from an access key's wrap value. The word list is the BIP-39 English list.",
		"account_id":  vAnn.String(),
		"prf_input":   V{"note": "SHA-256(\"churr sealed unlock v1\"), the first evaluation input of the WebAuthn PRF extension", "bytes": h(PRFInput())},
		"passkey": V{"prf_output": h(prf), "credential_id": h(credential), "salt": h(make([]byte, 32)), "info": h(labelled(LabelUnlockPasskey, vAnn[:], credential)),
			"unlock_private": h(passkeyPrivate), "unlock_public": h(X25519Public(passkeyPrivate))},
		"phrase": V{"entropy": h(entropy), "words": phrase, "salt": h(make([]byte, 32)), "info": h(labelled(LabelUnlockPhrase, vAnn[:])),
			"unlock_private": h(phrasePrivate), "unlock_public": h(X25519Public(phrasePrivate))},
		"access_key": V{"random": h(accessRandom), "wrap_salt": h([]byte(AccessKeySalt)), "wrap_info": h([]byte(AccessKeyWrapInfo)), "wrap": h(accessWrap),
			"salt": h(make([]byte, 32)), "info": h(labelled(LabelUnlockAccessKey)),
			"unlock_private": h(accessPrivate), "unlock_public": h(X25519Public(accessPrivate))},
		"phrases": phrases, "reading": reading,
	}

	// The two hand-overs to an app.
	app := Fixed("vector app box private", 32)
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	ho, err := SealHandover(X25519Public(app), vAnn, challenge, backupKey, Fixed("vector handover one-time", 32))
	must(err)
	got1, err := OpenHandover(app, vAnn, challenge, ho)
	must(err)
	_, e6 := OpenHandover(app, vAnn, "another-challenge", ho)
	refused(e6, "a hand-over under another challenge")
	pairingSecret := Fixed("vector pairing secret", 32)
	pho, err := SealPairingHandover(X25519Public(app), pairingSecret, vAnn, backupKey, Fixed("vector pairing one-time", 32), Fixed("vector pairing salt", 16))
	must(err)
	got2, err := OpenPairingHandover(app, pairingSecret, vAnn, pho)
	must(err)
	want(bytes.Equal(got1, backupKey) && bytes.Equal(got2, backupKey) && len(ho) == 80 && len(pho) == 112, "hand-over round trips")
	_, e7 := OpenPairingHandover(app, Fixed("vector another pairing secret", 32), vAnn, pho)
	refused(e7, "a pairing hand-over without the secret")
	inner, _ := Seal(X25519Public(app), LabelSealPairing, vAnn[:], backupKey, Fixed("vector pairing one-time", 32))
	handover = V{
		"description": "The hand-overs of the backup key to an app (SEALED.md sections 13.3 and 13.7).",
		"account_id":  vAnn.String(), "backup_key": h(backupKey), "app_box_private": h(app), "app_box_key": h(X25519Public(app)),
		"sign_in": V{"note": "seal(app box key, \"churr seal handover v1\", account_id || the ASCII bytes of the PKCE code challenge, backup key)",
			"code_challenge": challenge, "one_time_private": h(Fixed("vector handover one-time", 32)), "context": h(append(append([]byte{}, vAnn[:]...), challenge...)), "handover": h(ho),
			"refused_with_code_challenge": "another-challenge"},
		"pairing": V{"note": "lock(pairing secret, \"churr lock pairing v1\", account_id, seal(app box key, \"churr seal pairing v1\", account_id, backup key)), 112 bytes",
			"pairing_secret": h(pairingSecret), "one_time_private": h(Fixed("vector pairing one-time", 32)), "salt": h(Fixed("vector pairing salt", 16)),
			"inner": h(inner), "handover": h(pho), "refused_with_pairing_secret": h(Fixed("vector another pairing secret", 32))},
	}

	// The unlock request and its code.
	ur := &UnlockRequest{Format: 1, AccountID: vAnn, DeviceID: vAnnWeb.id, SignKey: Public(vAnnWeb.sign), BoxKey: X25519Public(vAnnWeb.box),
		Platform: PlatformWeb, At: vTime, Proof: DeviceProof(vAnnWeb.sign, vAnn, vAnnWeb.id, X25519Public(vAnnWeb.box))}
	raw, err := ur.Encode()
	must(err)
	_, err = CheckUnlockRequest(raw)
	must(err)
	code := ApprovalCode(raw)
	swappedKeys := *ur
	swappedKeys.BoxKey = X25519Public(Fixed("vector the server's own box key", 32))
	swappedRaw, _ := swappedKeys.Encode()
	_, e8 := CheckUnlockRequest(swappedRaw)
	refused(e8, "an unlock request whose keys were swapped")
	want(ApprovalCode(swappedRaw) != code, "another request has another code")
	sum := sha256.Sum256(labelled(LabelDeviceApproval, raw))
	answer, err := MakeLockedCopy(ur.BoxKey, vAnn, 1, WayDevice, ur.DeviceID, backupKey, vAnnKey, Fixed("vector answer one-time", 32))
	must(err)
	unlock = V{
		"description": "The unlock request of SEALED.md section 13.7, its 20-character code, and the answer.",
		"fields": V{"format": 1, "account_id": vAnn.String(), "device_id": ur.DeviceID.String(), "sign_key": h(ur.SignKey), "box_key": h(ur.BoxKey),
			"platform": ur.Platform, "at": ur.At, "proof": h(ur.Proof)},
		"request": h(raw), "code_hash": h(sum[:]), "code": code,
		"typed_forms_that_match": []string{code, "  " + toLowerSpaced(code)},
		"answer": V{"note": "a locked copy of kind device for the asking device's box key", "backup_key": h(backupKey), "account_key_seed": h(vAnnKey.Seed()),
			"key_version": 1, "one_time_private": h(Fixed("vector answer one-time", 32)), "locked_copy": h(answer), "device_box_private": h(vAnnWeb.box)},
		"refusals": []V{{"why": "the box key was swapped for another: the proof no longer verifies, and the code is different", "request": h(swappedRaw), "code": ApprovalCode(swappedRaw)}},
	}
	want(NormalizeApprovalCode("  "+toLowerSpaced(code)) == code, "a typed code is normalised")
	return account, record, copies, keys, handover, unlock
}

// toLowerSpaced writes a code as a person might type it: lower case, with
// spaces for hyphens.
func toLowerSpaced(code string) string {
	out := []byte(code)
	for i, c := range out {
		switch {
		case c == '-':
			out[i] = ' '
		case c >= 'A' && c <= 'Z':
			out[i] = c + 32
		}
	}
	return string(out)
}

func compareVectors() V {
	a, b := CompareHalf(Public(vAnnKey), vAnn, vServer), CompareHalf(Public(vBobKey), vBob, vServer)
	other := CompareHalf(Public(vBobKey), vBob, "other.example.org")
	first := sha256.Sum256(labelled(LabelCompare, Public(vAnnKey), vAnn[:], []byte(vServer)))
	return V{
		"description": "The comparison code of SEALED.md section 15: each account's 30-digit half, and the 60 digits both devices show.",
		"rounds":      compareRounds,
		"halves": []V{
			{"account_id": vAnn.String(), "server": vServer, "account_key": h(Public(vAnnKey)), "first_hash": h(first[:]), "half": a},
			{"account_id": vBob.String(), "server": vServer, "account_key": h(Public(vBobKey)), "half": b},
			{"account_id": vBob.String(), "server": "other.example.org", "account_key": h(Public(vBobKey)), "half": other},
		},
		"code": CompareCode(a, b), "code_is_the_same_from_both_sides": CompareCode(a, b) == CompareCode(b, a),
	}
}

func callVectors() V {
	e := buildEpoch()
	secret := CallSecret(e.secret, vConv, 1, vCall)
	senders := V{}
	for _, d := range []vDevice{vAnnPhone, vAnnWeb, vBobPhone} {
		senders[d.id.String()] = h(CallSenderKey(secret, vConv, 1, d.id))
	}
	return V{
		"description":     "Call keys of SEALED.md sections 5.2 and 10. They come from the epoch secret, never from the history secret. The frame format is libwebrtc's and is not covered here until section 10's list has been verified.",
		"conversation_id": vConv.String(), "key_epoch": 1, "call_id": vCall.String(), "epoch_secret": h(e.secret), "salt": h(epochSalt(vConv, 1)),
		"call_secret": h(secret), "call_sender_keys": senders,
		"key_index": []V{{"key_epoch": 1, "index": CallKeyIndex(1)}, {"key_epoch": 16, "index": CallKeyIndex(16)}, {"key_epoch": 35, "index": CallKeyIndex(35)}},
	}
}

// Vectors builds every vector file: its name and its contents.
func Vectors() (files map[string]any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("ref: the vectors went wrong: %v\n%s", p, debug.Stack())
		}
	}()
	steps, cast, err := Transcript()
	if err != nil {
		return nil, err
	}
	account, record, copies, keys, handover, unlock := backupVectors()
	return map[string]any{
		"primitives.json":      primitiveVectors(),
		"encoding.json":        encodingVectors(),
		"seal.json":            sealVectors(),
		"lock.json":            lockVectors(),
		"signatures.json":      signatureVectors(),
		"device_list.json":     deviceListVectors(),
		"epoch_record.json":    epochVectors(),
		"message.json":         messageVectors(),
		"file.json":            fileVectors(),
		"account_record.json":  account,
		"backup_record.json":   record,
		"locked_copy.json":     copies,
		"unlock_keys.json":     keys,
		"handover.json":        handover,
		"unlock_request.json":  unlock,
		"comparison_code.json": compareVectors(),
		"call_keys.json":       callVectors(),
		"transcript.json": V{
			"description": "A whole conversation between two accounts with several devices: every byte the server stores, in order, and what each device holds and shows after each step. README.md says how to replay it.",
			"server":      tServer, "conversation_id": tConv.String(),
			"accounts": V{"ann": tAnn.String(), "bob": tBob.String()},
			"devices":  cast, "start_clock": tStart,
			"passkey": V{"passkey_id": tPasskey.String()},
			"steps":   steps,
		},
	}, nil
}

// EncodeVector writes one vector file's contents the way the files are
// committed: indented JSON with a final newline.
func EncodeVector(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteVectors writes testdata/sealed: every vector file, from fixed
// inputs, so the output is the same every time.
func WriteVectors(dir string) error {
	files, err := Vectors()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, v := range files {
		raw, err := EncodeVector(v)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}
