// Command healthcheck is the container HEALTHCHECK for images built from
// deploy/Dockerfile. The runtime image is distroless — no shell, no curl —
// so the probe is a 30-line static binary: GET the URL given as the only
// argument (default the api's /healthz), exit 0 on 200, 1 on anything else.
// Both the api (HTTP_ADDR) and the worker (METRICS_ADDR) serve /healthz.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	url := "http://127.0.0.1:8080/healthz"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", url, res.Status)
		os.Exit(1)
	}
}
