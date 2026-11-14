package blob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// S3Config says how to reach an S3-compatible object store: Amazon S3, or
// anything that speaks the same protocol, such as MinIO or Garage. An admin
// enters it in the console (docs/OPERATIONS.md section 10) and it is kept
// in the settings table, with the secret sealed.
type S3Config struct {
	// Endpoint is the store's address, such as "s3.eu-central-1.amazonaws.com"
	// or "https://minio.internal:9000".
	Endpoint string
	// Region is the region the bucket is in. Stores other than Amazon
	// usually accept anything here.
	Region string
	// Bucket is the private bucket file contents go in.
	Bucket string
	// AccessKey is the access key identifier, and SecretKey its secret.
	// The key needs to get, put, delete and list objects in Bucket and
	// nothing else.
	AccessKey string
	SecretKey string
	// PathStyle addresses the bucket as endpoint/bucket and not as
	// bucket.endpoint. Most stores that are not Amazon need it.
	PathStyle bool
	// Prefix is put in front of every object name, so that one bucket can
	// be shared with something else. It may be empty.
	Prefix string
	// TmpDir is where an upload is kept while its length and hash are
	// worked out, before it is sent. Empty means the system's.
	TmpDir string
	// Client makes the requests; nil means one with sensible timeouts.
	Client *http.Client
}

// s3Store keeps blobs as objects. It speaks the four requests it needs
// (put, get, delete, list) itself, signed with AWS Signature Version 4, and
// uses nothing outside the standard library: the client module
// docs/STRUCTURE.md allows would bring some twenty modules with it for what
// is here a few hundred lines.
type s3Store struct {
	cfg    S3Config
	base   *url.URL // scheme and host, with the bucket in the host or the path
	bucket string   // "/bucket" for path-style addressing, "" otherwise
	client *http.Client
	now    func() time.Time
}

// S3 keeps blobs as objects in an S3-compatible store. Keys are
// "s3:<object name>", where the object name includes the configured
// prefix, so a blob stays findable if the prefix setting is changed later.
//
// A store that cannot be reached gives model.ErrUnavailable, an object that
// does not exist model.ErrNotFound, and a key that does not begin with
// "s3:" ErrBadKey.
func S3(cfg S3Config) (Blobs, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("blob: object storage needs an endpoint, a bucket, an access key and its secret")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	endpoint := cfg.Endpoint
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("blob: %q is not an address object storage can be reached at", cfg.Endpoint)
	}
	s := &s3Store{cfg: cfg, base: &url.URL{Scheme: u.Scheme, Host: u.Host}, client: cfg.Client, now: time.Now}
	if cfg.PathStyle {
		s.bucket = "/" + cfg.Bucket
	} else {
		s.base.Host = cfg.Bucket + "." + u.Host
	}
	if s.client == nil {
		s.client = &http.Client{Timeout: 10 * time.Minute}
	}
	return s, nil
}

// objectName turns a key into the object it names.
func (s *s3Store) objectName(key string) (string, error) {
	name, ok := strings.CutPrefix(key, s3Prefix)
	if !ok || name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("%w: %q is not an object storage key", ErrBadKey, key)
	}
	return name, nil
}

// do sends one signed request. query is the raw query string, already in
// the canonical form (sorted, encoded); payloadHash is the hex SHA-256 of
// the body.
func (s *s3Store) do(ctx context.Context, method, object, query string, body io.Reader, length int64, payloadHash string) (*http.Response, error) {
	u := *s.base
	u.Path = s.bucket + "/" + object
	u.RawPath = s.bucket + "/" + uriEncode(object, false)
	u.RawQuery = query
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = length
	}
	headers := map[string]string{
		"host":                 u.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           s.now().UTC().Format("20060102T150405Z"),
	}
	auth := signV4(method, u.EscapedPath(), query, headers, payloadHash, s.cfg.AccessKey, s.cfg.SecretKey, s.cfg.Region, "s3")
	for k, v := range headers {
		if k != "host" {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Authorization", auth)
	res, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("blob: object storage could not be reached (%v): %w", err, model.ErrUnavailable)
	}
	return res, nil
}

// failed turns an answer that is not a success into an error, with what
// the store said about it.
func failed(res *http.Response, doing string) error {
	defer res.Body.Close()
	said, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	xml.Unmarshal(said, &e)
	why := strings.TrimSpace(e.Code + " " + e.Message)
	if why == "" {
		why = res.Status
	}
	err := fmt.Errorf("blob: %s in object storage: %s", doing, why)
	if res.StatusCode >= 500 {
		return fmt.Errorf("%w: %w", err, model.ErrUnavailable)
	}
	return err
}

func (s *s3Store) Put(ctx context.Context, r io.Reader) (string, int64, []byte, error) {
	// A signed upload states its length and hash before the first byte, so
	// the content is written to a temporary file first. That is also what
	// makes a reader that fails part-way (LimitReader) store nothing.
	tmp, err := os.CreateTemp(s.cfg.TmpDir, "s3-put-*")
	if err != nil {
		return "", 0, nil, fmt.Errorf("blob: making a temporary file: %w", err)
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), ctxReader{ctx, r})
	if err != nil {
		return "", 0, nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, nil, err
	}
	raw := make([]byte, nameBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", 0, nil, err
	}
	name := hex.EncodeToString(raw)
	object := strings.TrimPrefix(s.cfg.Prefix+name[0:2]+"/"+name, "/")
	sum := hash.Sum(nil)
	var body io.Reader = io.NopCloser(tmp)
	if size == 0 {
		// A body of no bytes must be said to be none, or it would be sent
		// in chunks, which a signed upload cannot be.
		body = http.NoBody
	}
	res, err := s.do(ctx, http.MethodPut, object, "", body, size, hex.EncodeToString(sum))
	if err != nil {
		return "", 0, nil, err
	}
	if res.StatusCode/100 != 2 {
		return "", 0, nil, failed(res, "storing a file")
	}
	res.Body.Close()
	return s3Prefix + object, size, sum, nil
}

// emptyHash is the SHA-256 of nothing, the payload hash of a request with
// no body.
const emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.objectName(key)
	if err != nil {
		return nil, err
	}
	res, err := s.do(ctx, http.MethodGet, object, "", nil, 0, emptyHash)
	if err != nil {
		return nil, err
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		res.Body.Close()
		return nil, fmt.Errorf("blob: %s is gone: %w", key, model.ErrNotFound)
	case res.StatusCode/100 != 2:
		return nil, failed(res, "reading a file")
	}
	return res.Body, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	object, err := s.objectName(key)
	if err != nil {
		return err
	}
	res, err := s.do(ctx, http.MethodDelete, object, "", nil, 0, emptyHash)
	if err != nil {
		return err
	}
	// A store answers 204 whether or not the object was there; some answer
	// 404 for one that was not, which is the same thing to us.
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		return failed(res, "deleting a file")
	}
	res.Body.Close()
	return nil
}

// List calls fn for every blob under the configured prefix, with when it
// was last written. The janitor's sweep uses it to find blobs no row owns.
func (s *s3Store) List(ctx context.Context, fn func(key string, modified time.Time) error) error {
	token := ""
	for {
		q := url.Values{"list-type": {"2"}}
		if s.cfg.Prefix != "" {
			q.Set("prefix", s.cfg.Prefix)
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		res, err := s.do(ctx, http.MethodGet, "", canonicalQuery(q), nil, 0, emptyHash)
		if err != nil {
			return err
		}
		if res.StatusCode/100 != 2 {
			return failed(res, "listing files")
		}
		var page struct {
			IsTruncated bool   `xml:"IsTruncated"`
			Next        string `xml:"NextContinuationToken"`
			Contents    []struct {
				Key          string    `xml:"Key"`
				LastModified time.Time `xml:"LastModified"`
			} `xml:"Contents"`
		}
		err = xml.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&page)
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("blob: object storage sent a list that could not be read: %w", err)
		}
		for _, c := range page.Contents {
			if err := fn(s3Prefix+c.Key, c.LastModified); err != nil {
				return err
			}
		}
		if !page.IsTruncated || page.Next == "" {
			return nil
		}
		token = page.Next
	}
}

// CheckS3 is the admin console's "Test": it writes, reads back and deletes
// one small object, and says in a plain sentence what went wrong if
// anything did (docs/OPERATIONS.md section 10).
func CheckS3(ctx context.Context, cfg S3Config) error {
	b, err := S3(cfg)
	if err != nil {
		return err
	}
	content := []byte("Churr storage test. This object is deleted again at once.\n")
	key, _, _, err := b.Put(ctx, bytes.NewReader(content))
	if err != nil {
		return err
	}
	// Whatever happens next, do not leave the object behind.
	defer b.Delete(context.WithoutCancel(ctx), key)
	r, err := b.Get(ctx, key)
	if err != nil {
		return err
	}
	got, err := io.ReadAll(io.LimitReader(r, 1024))
	r.Close()
	if err != nil {
		return err
	}
	if !bytes.Equal(got, content) {
		return errors.New("blob: object storage gave back something other than what was written")
	}
	return b.Delete(ctx, key)
}

// signV4 makes the Authorization header of AWS Signature Version 4 for a
// request. path is the URI-encoded path, query the canonical query string,
// and headers every header to sign, by lower-case name.
func signV4(method, path, query string, headers map[string]string, payloadHash, accessKey, secretKey, region, service string) string {
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canon strings.Builder
	for _, k := range names {
		canon.WriteString(k + ":" + strings.Join(strings.Fields(headers[k]), " ") + "\n")
	}
	signed := strings.Join(names, ";")
	if path == "" {
		path = "/"
	}
	request := strings.Join([]string{method, path, query, canon.String(), signed, payloadHash}, "\n")
	amzDate := headers["x-amz-date"]
	day := amzDate[:8]
	scope := day + "/" + region + "/" + service + "/aws4_request"
	digest := sha256.Sum256([]byte(request))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(digest[:])
	key := signingKey(secretKey, day, region, service)
	return "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + scope + ", SignedHeaders=" + signed +
		", Signature=" + hex.EncodeToString(hmacSHA256(key, toSign))
}

// signingKey derives the key a day's requests to one service are signed
// with.
func signingKey(secret, day, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), day)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	return hmacSHA256(k, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// uriEncode encodes a string the way Signature Version 4 wants: everything
// but letters, digits, '-', '.', '_' and '~' as %XX in upper case. A slash
// is kept in a path and encoded in a query value.
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteString("%" + strings.ToUpper(strconv.FormatUint(uint64(c)+0x100, 16)[1:]))
		}
	}
	return b.String()
}

// canonicalQuery writes query parameters sorted by name and encoded as
// Signature Version 4 wants.
func canonicalQuery(q url.Values) string {
	names := make([]string, 0, len(q))
	for k := range q {
		names = append(names, k)
	}
	sort.Strings(names)
	var parts []string
	for _, k := range names {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}
