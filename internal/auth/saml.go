package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"

	"github.com/tikaydaes/churr/internal/model"
)

// SAML 2.0, in the narrowest useful profile (docs/AUTH.md section 7.3):
// sign-in started here only, the request by HTTP-Redirect and the answer by
// HTTP-POST, no encrypted assertions, no single logout, no artifacts.
//
// crewjam/saml builds the AuthnRequest, reads the provider's metadata and
// writes ours. The answer is validated here, step by step as the document
// lists them, with goxmldsig (the XML signature library crewjam itself
// uses) verifying the assertion's signature and handing back the canonical
// bytes it covered, which are the only thing values are read from.

// samlConfig is the `config` of a SAML provider.
type samlConfig struct {
	EntityID string `json:"entity_id,omitempty"`
	// SSOURL is the provider's single sign-on address (HTTP-Redirect).
	SSOURL string `json:"sso_url,omitempty"`
	// Certificates are the provider's signing certificates, in PEM. They
	// are trusted because the admin put them there; their expiry dates
	// and issuers are not checked.
	Certificates []string `json:"certificates,omitempty"`
	// MetadataXML and MetadataURL are how an admin gives the three fields
	// above in one go. The metadata is loaded once and not refreshed.
	MetadataXML string `json:"metadata_xml,omitempty"`
	MetadataURL string `json:"metadata_url,omitempty"`
	// Attribute names. The subject is the NameID unless one is named.
	SubjectAttribute  string `json:"subject_attribute,omitempty"`
	UsernameAttribute string `json:"username_attribute,omitempty"`
	NameAttribute     string `json:"name_attribute,omitempty"`
	EmailAttribute    string `json:"email_attribute,omitempty"`
	// What counts as the provider's statement of multi-factor: one of
	// these context classes, or the attribute holding one of the values.
	MFAContexts  []string `json:"mfa_contexts,omitempty"`
	MFAAttribute string   `json:"mfa_attribute,omitempty"`
	MFAValues    []string `json:"mfa_values,omitempty"`
	// SignRequests makes the server sign its requests, for a provider
	// that wants that.
	SignRequests bool `json:"sign_requests,omitempty"`
	// SPCertificate is this server's own certificate for the provider,
	// made by the server. Read-only.
	SPCertificate string `json:"sp_certificate,omitempty"`
}

const (
	samlMaxResponse = 256 << 10
	nsProtocol      = "urn:oasis:names:tc:SAML:2.0:protocol"
	nsAssertion     = "urn:oasis:names:tc:SAML:2.0:assertion"
	nsDsig          = "http://www.w3.org/2000/09/xmldsig#"
	statusSuccess   = "urn:oasis:names:tc:SAML:2.0:status:Success"
	methodBearer    = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
	algEnveloped    = "http://www.w3.org/2000/09/xmldsig#enveloped-signature"
	algExcC14N      = "http://www.w3.org/2001/10/xml-exc-c14n#"
	formatTransient = "urn:oasis:names:tc:SAML:2.0:nameid-format:transient"
)

// The signature and digest algorithms accepted. SHA-1 is refused by not
// being here.
var (
	samlSignatureAlgs = map[string]bool{
		dsig.RSASHA256SignatureMethod: true, dsig.RSASHA384SignatureMethod: true, dsig.RSASHA512SignatureMethod: true,
		dsig.ECDSASHA256SignatureMethod: true, dsig.ECDSASHA384SignatureMethod: true, dsig.ECDSASHA512SignatureMethod: true,
	}
	samlDigestAlgs = map[string]bool{
		"http://www.w3.org/2001/04/xmlenc#sha256": true, "http://www.w3.org/2001/04/xmldsig-more#sha384": true,
		"http://www.w3.org/2001/04/xmlenc#sha512": true,
	}
)

// parseCertificates reads certificates given as PEM or as bare base64.
func parseCertificates(list []string) ([]*x509.Certificate, []string, error) {
	var certs []*x509.Certificate
	var pems []string
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		var der []byte
		if block, _ := pem.Decode([]byte(raw)); block != nil {
			der = block.Bytes
		} else {
			clean := strings.Map(func(r rune) rune {
				if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
					return -1
				}
				return r
			}, raw)
			var err error
			if der, err = base64.StdEncoding.DecodeString(clean); err != nil {
				return nil, nil, errors.New("a certificate is neither PEM nor base64")
			}
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, nil, errors.New("a certificate could not be read")
		}
		certs = append(certs, cert)
		pems = append(pems, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	}
	return certs, pems, nil
}

// samlConfigure checks a config an admin entered, loads the provider's
// metadata if that is how it was given, makes this server's signing key the
// first time, and returns the config and sealed key to store.
func (s *Service) samlConfigure(ctx context.Context, p model.SsoProvider, raw json.RawMessage) (json.RawMessage, []byte, error) {
	bad := func(msg string) (json.RawMessage, []byte, error) { return nil, nil, model.Invalid("config", msg) }
	var c samlConfig
	if err := strictJSON(raw, &c); err != nil {
		return bad("A SAML config has entity_id, sso_url and certificates, or metadata_xml or metadata_url, and optionally attribute names and multi-factor values.")
	}
	if c.MetadataXML == "" && c.MetadataURL != "" {
		if !httpsURL(c.MetadataURL) {
			return bad("metadata_url must be an https address.")
		}
		fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req, err := newGet(fctx, c.MetadataURL)
		if err != nil {
			return bad("metadata_url cannot be read.")
		}
		resp, err := s.httpc.Do(req)
		if err != nil {
			return nil, nil, model.ErrUnavailable.With("The provider's metadata could not be fetched.")
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			return nil, nil, model.ErrUnavailable.With("The provider's metadata could not be fetched.")
		}
		c.MetadataXML = string(body)
	}
	if c.MetadataXML != "" {
		md, err := samlsp.ParseMetadata([]byte(c.MetadataXML))
		if err != nil || len(md.IDPSSODescriptors) == 0 {
			return bad("The metadata is not a SAML identity provider's.")
		}
		c.EntityID, c.SSOURL, c.Certificates = md.EntityID, "", nil
		for _, idp := range md.IDPSSODescriptors {
			for _, ep := range idp.SingleSignOnServices {
				if ep.Binding == saml.HTTPRedirectBinding && c.SSOURL == "" {
					c.SSOURL = ep.Location
				}
			}
			for _, kd := range idp.KeyDescriptors {
				if kd.Use == "signing" || kd.Use == "" {
					for _, xc := range kd.KeyInfo.X509Data.X509Certificates {
						c.Certificates = append(c.Certificates, xc.Data)
					}
				}
			}
		}
		c.MetadataXML = ""
	}
	c.EntityID = strings.TrimSpace(c.EntityID)
	if c.EntityID == "" {
		return bad("entity_id is needed.")
	}
	if !httpsURL(c.SSOURL) {
		return bad("sso_url must be the provider's https sign-on address for the HTTP-Redirect binding.")
	}
	certs, pems, err := parseCertificates(c.Certificates)
	if err != nil {
		return bad(err.Error())
	}
	if len(certs) == 0 {
		return bad("At least one of the provider's signing certificates is needed.")
	}
	c.Certificates = pems
	if (c.MFAAttribute == "") != (len(c.MFAValues) == 0) {
		return bad("mfa_attribute and mfa_values go together.")
	}
	// This server's own key and certificate for this provider are made
	// once and kept through later changes.
	sealed := p.Secret
	var old samlConfig
	json.Unmarshal(p.Config, &old)
	c.SPCertificate = old.SPCertificate
	if len(sealed) == 0 || c.SPCertificate == "" {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, nil, err
		}
		serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
		tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: s.cfg.Hostname},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			return nil, nil, err
		}
		pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, nil, err
		}
		if sealed, err = s.sealProviderSecret(p.ID, pkcs8); err != nil {
			return nil, nil, err
		}
		c.SPCertificate = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	out, err := json.Marshal(c)
	return out, sealed, err
}

// samlEntityID is this server's entity ID for a provider: the address of
// its metadata.
func (s *Service) samlEntityID(p model.SsoProvider) string {
	return s.cfg.Origin + "/api/v1/sso/" + p.ID + "/metadata"
}

// samlProvider reads a provider's config and builds the library's view of
// this server as a service provider.
func (s *Service) samlProvider(p model.SsoProvider) (*saml.ServiceProvider, samlConfig, error) {
	var c samlConfig
	if err := json.Unmarshal(p.Config, &c); err != nil {
		return nil, c, err
	}
	acs, err := url.Parse(s.callbackURL(p))
	if err != nil {
		return nil, c, err
	}
	meta, _ := url.Parse(s.samlEntityID(p))
	sp := &saml.ServiceProvider{
		EntityID: s.samlEntityID(p), AcsURL: *acs, MetadataURL: *meta,
		AuthnNameIDFormat: saml.PersistentNameIDFormat,
		IDPMetadata: &saml.EntityDescriptor{EntityID: c.EntityID, IDPSSODescriptors: []saml.IDPSSODescriptor{{
			SingleSignOnServices: []saml.Endpoint{{Binding: saml.HTTPRedirectBinding, Location: c.SSOURL}},
		}}},
	}
	if certs, _, err := parseCertificates([]string{c.SPCertificate}); err == nil && len(certs) == 1 {
		sp.Certificate = certs[0]
	}
	if c.SignRequests {
		der, err := s.providerSecret(p)
		if err != nil {
			return nil, c, err
		}
		key, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, c, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, c, errors.New("the stored signing key is not an RSA key")
		}
		sp.Key, sp.SignatureMethod = rsaKey, dsig.RSASHA256SignatureMethod
	}
	return sp, c, nil
}

// samlStart builds the redirect that carries an AuthnRequest to the
// provider: a random ID of 160 bits, the callback as the consumer address,
// a persistent NameID asked for, and the request handle as RelayState.
func (s *Service) samlStart(p model.SsoProvider, handle string, w *waiting) (string, error) {
	sp, c, err := s.samlProvider(p)
	if err != nil {
		return "", err
	}
	req, err := sp.MakeAuthenticationRequest(c.SSOURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", err
	}
	w.requestID = req.ID
	u, err := req.Redirect(handle, sp)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// SamlMetadata is this server's service-provider metadata for a provider.
func (s *Service) SamlMetadata(ctx context.Context, addr, providerID string) ([]byte, error) {
	if err := limited(s.limitWellKnown, addr); err != nil {
		return nil, err
	}
	p, err := s.store.Identities().GetProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if p.Kind != model.ProviderSAML {
		return nil, model.ErrNotFound
	}
	sp, _, err := s.samlProvider(p)
	if err != nil {
		return nil, err
	}
	out, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	return append([]byte(xml.Header), out...), err
}

// samlTest checks that the stored config can still be used.
func (s *Service) samlTest(p model.SsoProvider) error {
	_, c, err := s.samlProvider(p)
	if err != nil {
		return errors.New("the stored config cannot be read")
	}
	certs, _, err := parseCertificates(c.Certificates)
	if err != nil || len(certs) == 0 {
		return errors.New("no signing certificate of the provider is stored")
	}
	if !httpsURL(c.SSOURL) || c.EntityID == "" {
		return errors.New("the provider's entity ID or sign-on address is missing")
	}
	return nil
}

// SamlCallback receives a SAML response. Only a signed assertion from the
// configured provider that answers a request this server made is accepted.
func (s *Service) SamlCallback(ctx context.Context, addr, providerID, response, relayState, cookie, label string) (SsoResult, error) {
	if err := limited(s.limitSsoCallback, addr); err != nil {
		return SsoResult{}, err
	}
	p, err := s.store.Identities().GetProvider(ctx, providerID)
	if err != nil {
		return SsoResult{}, err
	}
	res := SsoResult{ProviderName: p.Name}
	if p.Kind != model.ProviderSAML || !p.Enabled {
		return res, model.ErrNotFound
	}
	w, who, err := s.samlValidate(p, response, relayState, cookie, time.Now())
	if err != nil {
		return res, s.ssoFailed(p, addr, "SAML response", err)
	}
	return s.finishSso(ctx, addr, p, w, who, label)
}

// child returns the direct children of el with this namespace and name.
func children(el *etree.Element, ns, tag string) []*etree.Element {
	var out []*etree.Element
	for _, c := range el.ChildElements() {
		if c.Tag == tag && c.NamespaceURI() == ns {
			out = append(out, c)
		}
	}
	return out
}

func child(el *etree.Element, ns, tag string) *etree.Element {
	if list := children(el, ns, tag); len(list) > 0 {
		return list[0]
	}
	return nil
}

// walk visits el and every element below it.
func walk(el *etree.Element, fn func(*etree.Element)) {
	fn(el)
	for _, c := range el.ChildElements() {
		walk(c, fn)
	}
}

// textOf is the whole character content of an element with comments
// ignored, so a comment placed inside a name cannot cut it short. An
// element with child elements has no text of its own.
func textOf(el *etree.Element) (string, error) {
	var b strings.Builder
	for _, tok := range el.Child {
		switch t := tok.(type) {
		case *etree.CharData:
			b.WriteString(t.Data)
		case *etree.Comment:
		default:
			return "", fmt.Errorf("unexpected content inside <%s>", el.Tag)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func attr(el *etree.Element, name string) string {
	if a := el.SelectAttr(name); a != nil {
		return a.Value
	}
	return ""
}

func samlTime(v string) (time.Time, error) { return time.Parse(time.RFC3339Nano, v) }

// samlValidate validates a response in the order of docs/AUTH.md section
// 7.3. Any failure refuses the whole response.
func (s *Service) samlValidate(p model.SsoProvider, encoded, relayState, cookie string, now time.Time) (waiting, asserted, error) {
	var (
		w   waiting
		who asserted
	)
	fail := func(format string, args ...any) (waiting, asserted, error) {
		return waiting{}, asserted{}, fmt.Errorf(format, args...)
	}
	sp, c, err := s.samlProvider(p)
	if err != nil {
		return fail("config: %v", err)
	}
	acs, entityID := sp.AcsURL.String(), sp.EntityID

	// 1. Size and parsing. Document type declarations and entities are
	// refused before the parser sees the document.
	if len(encoded) > base64.StdEncoding.EncodedLen(samlMaxResponse)+1024 {
		return fail("the response is too large")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, encoded))
	if err != nil || len(raw) == 0 {
		return fail("the response is not base64")
	}
	if len(raw) > samlMaxResponse {
		return fail("the response is too large")
	}
	upper := bytes.ToUpper(raw)
	if bytes.Contains(upper, []byte("<!DOCTYPE")) || bytes.Contains(upper, []byte("<!ENTITY")) {
		return fail("the response carries a document type declaration")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return fail("the response is not well-formed XML: %v", err)
	}
	for _, tok := range doc.Child {
		if _, isDirective := tok.(*etree.Directive); isDirective {
			return fail("the response carries a directive")
		}
	}
	root := doc.Root()
	if root == nil || root.Tag != "Response" || root.NamespaceURI() != nsProtocol {
		return fail("the document is not a SAML response")
	}

	// 2. Asked for: RelayState names a waiting request for this provider,
	// made by this browser. It is taken out of memory here.
	if w, err = s.takeWaiting(relayState, p.ID, cookie); err != nil {
		return fail("%v", err)
	}

	// 3. The response envelope.
	status := child(root, nsProtocol, "Status")
	if status == nil {
		return fail("the response has no status")
	}
	if code := child(status, nsProtocol, "StatusCode"); code == nil || attr(code, "Value") != statusSuccess {
		return fail("the status is not Success")
	}
	if attr(root, "Destination") != acs {
		return fail("Destination is not this server's callback")
	}
	if w.requestID == "" || attr(root, "InResponseTo") != w.requestID {
		return fail("InResponseTo does not match the request")
	}
	if iss := child(root, nsAssertion, "Issuer"); iss != nil {
		if v, err := textOf(iss); err != nil || v != c.EntityID {
			return fail("the response's issuer is not the provider")
		}
	}

	// 4. Exactly one assertion, a direct child of the response, none
	// encrypted, and no two elements sharing an ID.
	var assertions []*etree.Element
	ids := map[string]bool{}
	var problem string
	walk(root, func(el *etree.Element) {
		if el.NamespaceURI() == nsAssertion && el.Tag == "Assertion" {
			assertions = append(assertions, el)
		}
		if el.Tag == "EncryptedAssertion" || el.Tag == "EncryptedID" || el.Tag == "EncryptedAttribute" {
			problem = "the response carries encrypted content, which is not supported"
		}
		for _, tok := range el.Child {
			if _, isComment := tok.(*etree.Comment); isComment {
				// A comment inside a name is how a signed value is
				// made to read as a shorter one. No provider needs
				// comments in an answer, so none is accepted.
				problem = "the response carries a comment"
			}
		}
		for _, a := range el.Attr {
			if strings.EqualFold(a.Key, "ID") {
				if a.Value == "" || ids[a.Value] {
					problem = "two elements share an ID"
				}
				ids[a.Value] = true
			}
		}
	})
	if problem != "" {
		return fail("%s", problem)
	}
	if len(assertions) != 1 || assertions[0].Parent() != root {
		return fail("the response must hold exactly one assertion, as its direct child")
	}
	assertion := assertions[0]
	assertionID := attr(assertion, "ID")
	if assertionID == "" {
		return fail("the assertion has no ID")
	}

	// 5. The assertion itself carries the signature, as its direct child,
	// over itself and nothing else. A signature over the response alone
	// is not enough.
	var signatures []*etree.Element
	walk(assertion, func(el *etree.Element) {
		if el.NamespaceURI() == nsDsig && el.Tag == "Signature" {
			signatures = append(signatures, el)
		}
	})
	if len(signatures) != 1 || signatures[0].Parent() != assertion {
		return fail("the assertion must carry exactly one signature, as its direct child")
	}
	info := child(signatures[0], nsDsig, "SignedInfo")
	if info == nil {
		return fail("the signature has no SignedInfo")
	}
	if m := child(info, nsDsig, "CanonicalizationMethod"); m == nil || attr(m, "Algorithm") != algExcC14N {
		return fail("the signature is not canonicalised exclusively")
	}
	if m := child(info, nsDsig, "SignatureMethod"); m == nil || !samlSignatureAlgs[attr(m, "Algorithm")] {
		return fail("the signature algorithm is not accepted")
	}
	refs := children(info, nsDsig, "Reference")
	if len(refs) != 1 || attr(refs[0], "URI") != "#"+assertionID {
		return fail("the signature must have exactly one reference, to the assertion itself")
	}
	if m := child(refs[0], nsDsig, "DigestMethod"); m == nil || !samlDigestAlgs[attr(m, "Algorithm")] {
		return fail("the digest algorithm is not accepted")
	}
	if transforms := child(refs[0], nsDsig, "Transforms"); transforms != nil {
		for _, t := range transforms.ChildElements() {
			if alg := attr(t, "Algorithm"); t.Tag != "Transform" || (alg != algEnveloped && alg != algExcC14N) {
				return fail("the signature uses a transform that is not accepted")
			}
		}
	}
	certs, _, err := parseCertificates(c.Certificates)
	if err != nil || len(certs) == 0 {
		return fail("no provider certificate is configured")
	}
	// The assertion is lifted out with the namespaces it inherits, then
	// verified under each configured certificate in turn. The key in the
	// document's own KeyInfo is never trusted: the library uses it only
	// to pick which configured certificate to try.
	nsCtx, err := etreeutils.NSBuildParentContext(assertion)
	if err != nil {
		return fail("namespaces: %v", err)
	}
	detached, err := etreeutils.NSDetatch(nsCtx, assertion)
	if err != nil {
		return fail("namespaces: %v", err)
	}
	var signed *etree.Element
	var verr error
	for _, cert := range certs {
		vctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}})
		// The certificate's own dates are not checked: it is trusted
		// because the admin put it there.
		vctx.Clock = dsig.NewFakeClockAt(cert.NotBefore)
		if signed, verr = vctx.Validate(detached); verr == nil {
			break
		}
	}
	if verr != nil || signed == nil {
		return fail("the assertion's signature does not verify under a configured certificate: %v", verr)
	}

	// 6. Use what was signed. From here on every value is read from the
	// canonical bytes the signature covered, parsed afresh, and never
	// from the original document. This is what defeats signature
	// wrapping.
	a := signed
	if a.Tag != "Assertion" || a.NamespaceURI() != nsAssertion || attr(a, "ID") != assertionID {
		return fail("what was signed is not the assertion")
	}

	// 7. The assertion's issuer.
	iss := child(a, nsAssertion, "Issuer")
	if iss == nil {
		return fail("the assertion has no issuer")
	}
	if v, err := textOf(iss); err != nil || v != c.EntityID {
		return fail("the assertion's issuer is not the provider")
	}

	// 8. Subject confirmation, with the 60 seconds of step 10.
	subject := child(a, nsAssertion, "Subject")
	if subject == nil {
		return fail("the assertion has no subject")
	}
	confirmed := false
	for _, sc := range children(subject, nsAssertion, "SubjectConfirmation") {
		data := child(sc, nsAssertion, "SubjectConfirmationData")
		if attr(sc, "Method") != methodBearer || data == nil {
			continue
		}
		until, err := samlTime(attr(data, "NotOnOrAfter"))
		if err != nil || attr(data, "Recipient") != acs || attr(data, "InResponseTo") != w.requestID {
			continue
		}
		if !now.Before(until.Add(clockAllowance)) {
			return fail("the subject confirmation has expired")
		}
		confirmed = true
	}
	if !confirmed {
		return fail("no bearer subject confirmation is addressed to this server for this request")
	}

	// 9. Conditions.
	cond := child(a, nsAssertion, "Conditions")
	if cond == nil {
		return fail("the assertion has no conditions")
	}
	if nb := attr(cond, "NotBefore"); nb != "" {
		t, err := samlTime(nb)
		if err != nil || now.Add(clockAllowance).Before(t) {
			return fail("the assertion is not valid yet")
		}
	}
	notAfter, err := samlTime(attr(cond, "NotOnOrAfter"))
	if err != nil || !now.Before(notAfter.Add(clockAllowance)) {
		return fail("the assertion has expired")
	}
	restrictions := children(cond, nsAssertion, "AudienceRestriction")
	if len(restrictions) == 0 {
		return fail("the assertion names no audience")
	}
	for _, r := range restrictions {
		named := false
		for _, aud := range children(r, nsAssertion, "Audience") {
			if v, err := textOf(aud); err == nil && v == entityID {
				named = true
			}
		}
		if !named {
			return fail("the assertion was made for another service")
		}
	}

	// 11. Once only: the assertion's ID is remembered until it could no
	// longer be valid.
	seen := p.ID + "\x00" + assertionID
	if _, replay := s.assertions.get(seen); replay {
		return fail("the assertion has been used before")
	}
	s.assertions.putUntil(seen, struct{}{}, notAfter.Add(clockAllowance))

	// 12. The authentication statement.
	stmt := child(a, nsAssertion, "AuthnStatement")
	if stmt == nil {
		return fail("the assertion has no authentication statement")
	}
	who = asserted{by: "saml"}
	if v := attr(stmt, "SessionNotOnOrAfter"); v != "" {
		t, err := samlTime(v)
		if err != nil {
			return fail("SessionNotOnOrAfter cannot be read")
		}
		who.sessionEnd = model.FromTime(t)
	}
	if actx := child(stmt, nsAssertion, "AuthnContext"); actx != nil {
		if ref := child(actx, nsAssertion, "AuthnContextClassRef"); ref != nil {
			if v, err := textOf(ref); err == nil && v != "" && contains(c.MFAContexts, v) {
				who.mfa = true
			}
		}
	}

	// Attributes.
	attrs := map[string][]string{}
	for _, st := range children(a, nsAssertion, "AttributeStatement") {
		for _, at := range children(st, nsAssertion, "Attribute") {
			name := attr(at, "Name")
			if _, known := attrs[name]; !known {
				attrs[name] = []string{}
			}
			for _, val := range children(at, nsAssertion, "AttributeValue") {
				v, err := textOf(val)
				if err != nil {
					return fail("attribute %q: %v", name, err)
				}
				attrs[name] = append(attrs[name], v)
			}
		}
	}
	first := func(name string) string {
		if name == "" || len(attrs[name]) == 0 {
			return ""
		}
		return attrs[name][0]
	}

	// The subject: the NameID, or the configured attribute.
	if c.SubjectAttribute != "" {
		if len(attrs[c.SubjectAttribute]) != 1 {
			return fail("the subject attribute must hold exactly one value")
		}
		who.subject = attrs[c.SubjectAttribute][0]
	} else {
		nameID := child(subject, nsAssertion, "NameID")
		if nameID == nil {
			return fail("the subject has no NameID")
		}
		if attr(nameID, "Format") == formatTransient {
			return fail("a transient NameID changes at every sign-in and cannot identify an account")
		}
		if who.subject, err = textOf(nameID); err != nil {
			return fail("NameID: %v", err)
		}
	}
	if who.subject == "" || len(who.subject) > 255 {
		return fail("the subject is missing or too long")
	}
	who.username, who.name, who.email = first(c.UsernameAttribute), first(c.NameAttribute), first(c.EmailAttribute)
	if p.GroupClaim != "" {
		who.groups, who.hasGroups = attrs[p.GroupClaim]
	}
	if c.MFAAttribute != "" {
		for _, v := range attrs[c.MFAAttribute] {
			if contains(c.MFAValues, v) {
				who.mfa = true
			}
		}
	}
	return w, who, nil
}
