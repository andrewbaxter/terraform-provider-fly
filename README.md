# Community fly.io Terraform provider

This was forked from the [original provider](https://github.com/fly-apps/terraform-provider-fly) in an attempt to mantain it via the community.

I'm not actively using Fly at the moment - I'll try to fix things if I have time, but otherwise I hope I'll be prompt in reviewing and merging PRs.

## Terraform Registry - andrewbaxter/fly

Terraform resources are documented on the Terraform Registry, published to [andrewbaxter/fly](https://registry.terraform.io/providers/andrewbaxter/fly/latest/docs).

## Contributions

My only request is try not to change more than is necessary in a PR (i.e. leave formatting alone, if there are multiple unrelated changes turn them into multiple MRs, etc).

Also unless it's a one or two line fix, it may be worth making an issue to discuss the fix first. There's a large backlog of refactoring this could use (ex: use `superfly/fly` client library, update graphql definitions, etc) and if you start doing one thing it may snowball. Talking things through first we may be able to come up with a plan to make incremental changes.

## Tips for adding functionality

Fly uses two APIs simultaneously: the old GraphQL API and the new "Machines" REST API (it also deals with apps, volumes, etc).

If you run the provider with `TF_LOG=DEBUG` it'll log all http requests for both GraphQL and REST queries (note that terraform messes with the escaping so it may be more of a rough approximation of what's sent).

You can compare requests to what `flyctl` does for the same operations. I cloned it and made the following changes locally to get it to do the same logging:

```patch
diff --git a/api/client.go b/api/client.go
index 0145d44d..7ddebdc0 100644
--- a/api/client.go
+++ b/api/client.go
@@ -6,6 +6,8 @@ import (
 	"encoding/json"
 	"errors"
 	"fmt"
+	"io"
+	"log"
 	"net/http"
 	"os"
 	"regexp"
@@ -230,7 +232,31 @@ func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
 	if t.EnableDebugTrace {
 		req.Header.Set("Fly-Force-Trace", "true")
 	}
-	return t.UnderlyingTransport.RoundTrip(req)
+
+	{
+		body, err := io.ReadAll(req.Body)
+		if err != nil {
+			return nil, err
+		}
+		log.Printf("GRAPHQL -> %s %s %s\nHeaders: %#v\nBody:\n%s", req.Method, req.Proto, req.URL.String(), req.Header, string(body))
+		req.Body = io.NopCloser(bytes.NewBuffer(body))
+	}
+
+	resp, err := t.UnderlyingTransport.RoundTrip(req)
+
+	if resp != nil {
+		body, err1 := io.ReadAll(resp.Body)
+		if err1 != nil {
+			if err != nil {
+				return resp, err
+			}
+			return resp, err1
+		}
+		log.Printf("GRAPHQL <- %s %s %s = %s\nHeaders: %#v\nBody:\n%s", req.Method, req.Proto, req.URL.String(), resp.Status, resp.Header, string(body))
+		resp.Body = io.NopCloser(bytes.NewBuffer(body))
+	}
+
+	return resp, err
 }

 func (t *Transport) tokens() *tokens.Tokens {
diff --git a/flaps/flaps.go b/flaps/flaps.go
index 94cccfaa..cd44fa3d 100644
--- a/flaps/flaps.go
+++ b/flaps/flaps.go
@@ -7,6 +7,7 @@ import (
 	"errors"
 	"fmt"
 	"io"
+	"log"
 	"net"
 	"net/http"
 	"net/url"
@@ -178,6 +179,7 @@ func (f *Client) _sendRequest(ctx context.Context, method, endpoint string, in,
 	}
 	req.Header.Set("User-Agent", f.userAgent)

+	log.Printf("DEBUG sendRequest %s %s\nHeaders: %#v\nReq body: %#v", method, endpoint, headers, in)
 	resp, err := f.httpClient.Do(req)
 	if err != nil {
 		return 0, err
```
