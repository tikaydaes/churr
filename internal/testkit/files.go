package testkit

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
)

// Part is one field of a multipart form. A part with a FileName is sent as
// a file.
type Part struct {
	Field    string
	FileName string
	Content  []byte
}

// Form sends a multipart form, as an upload does, and decodes the JSON
// answer.
func (c *Client) Form(method, path string, parts ...Part) (int, map[string]any) {
	c.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, p := range parts {
		var pw io.Writer
		var err error
		if p.FileName != "" {
			pw, err = w.CreateFormFile(p.Field, p.FileName)
		} else {
			pw, err = w.CreateFormField(p.Field)
		}
		if err != nil {
			c.t.Fatal(err)
		}
		pw.Write(p.Content)
	}
	w.Close()
	resp, raw := c.raw(method, path, &body, map[string]string{"Content-Type": w.FormDataContentType()})
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			out = map[string]any{"_raw": string(raw)}
		}
	}
	return resp.StatusCode, out
}

// Upload uploads a file to a conversation and returns the file object.
func (c *Client) Upload(conversation, name string, content []byte, more ...Part) map[string]any {
	c.t.Helper()
	parts := append([]Part{{Field: "file", FileName: name, Content: content}}, more...)
	status, out := c.Form("POST", "/api/v1/conversations/"+conversation+"/files", parts...)
	if status != 201 {
		c.t.Fatalf("upload of %s = %d %v", name, status, out)
	}
	return out["file"].(map[string]any)
}

// raw sends a request with a body that is not JSON.
func (c *Client) raw(method, path string, body io.Reader, headers map[string]string) (*http.Response, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.ts.URL+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	hc := c.http
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
		hc = &http.Client{}
	} else if !c.NoOrigin {
		req.Header.Set("Origin", c.ts.Origin)
	}
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}
