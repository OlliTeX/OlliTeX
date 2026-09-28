package s3x

import (
	"io"
	"net/http"
	"net/url"
)

// DoRaw executes an arbitrary S3 gateway request (bucket-scoped) and returns
// the successful response for the caller to consume (close resp.Body) or a
// classified error (mapErr) for non-2xx, with the error body already read and
// closed. hdr is applied verbatim (no auth/redirect decisions); body may be
// nil for bodyless verbs.
func (c *Client) DoRaw(method, bucket, key string, query url.Values, hdr http.Header, body io.Reader) (*http.Response, error) {
	req, err := c.newReq(method, c.urlv(bucket, key, query), body, hdr)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	sc := resp.StatusCode
	if sc < 200 || sc > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, mapErr(sc, b)
	}
	return resp, nil
}
