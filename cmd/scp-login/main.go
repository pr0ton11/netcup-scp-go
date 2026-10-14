// Command scp-login logs in to the netcup SCP REST API with the device authorization flow
// and writes the offline refresh token to a file, for a service to use with auth.NewTokenSource.
//
//	go run github.com/pr0ton11/netcup-scp-go/cmd/scp-login -o netcup-refresh-token
//
// It then checks the token against the API and prints the claims of the access token, which
// include the SCP user ID that the /api/v1/users/{userId}/... endpoints need.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/oauth2"

	"github.com/pr0ton11/netcup-scp-go/auth"
	"github.com/pr0ton11/netcup-scp-go/scp"
)

func main() {
	out := flag.String("o", "", "file to write the refresh token to (required, created with mode 0600)")
	flag.Parse()
	if *out == "" {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	tok, err := auth.Login(ctx, func(da *oauth2.DeviceAuthResponse) {
		fmt.Fprintf(os.Stderr, "Open %s and confirm the code %s.\n", da.VerificationURIComplete, da.UserCode)
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := auth.WriteRefreshToken(*out, tok.RefreshToken); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Refresh token written to %s.\n", *out)

	ts := auth.NewTokenSource(ctx, tok.RefreshToken, func(rt string) error {
		return auth.WriteRefreshToken(*out, rt)
	})
	client, err := scp.NewClientWithResponses(auth.Server, scp.WithHTTPClient(oauth2.NewClient(ctx, ts)))
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.GetApiV1ServersWithResponse(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "GET /api/v1/servers: %s\n", resp.Status())

	claims, err := jwtClaims(tok.AccessToken)
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(claims)
}

// jwtClaims decodes the payload of a JWT without checking its signature. It is only used to
// show the claims the server put in a token it just issued to us.
func jwtClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("access token is not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode access token: %w", err)
	}
	var claims map[string]any
	return claims, json.Unmarshal(b, &claims)
}
