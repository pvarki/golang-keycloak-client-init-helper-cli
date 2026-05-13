package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// Version is incremented using bump2version
const Version = "1.1.0+260513"

// safeJoin joins base with parts and verifies the result stays inside base.
func safeJoin(base string, parts ...string) (string, error) {
	abs, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	joined := filepath.Clean(filepath.Join(append([]string{abs}, parts...)...))
	if !strings.HasPrefix(joined, abs+string(os.PathSeparator)) && joined != abs {
		return "", fmt.Errorf("path escapes base directory: %s", joined)
	}
	return joined, nil
}

func fileExist(pth string) bool {
	if _, err := os.Stat(pth); err == nil {
		return true
	} else if errors.Is(err, os.ErrNotExist) {
		return false
	} else {
		log.Errorf("Can't verify %s: %s", pth, err)
		return false
	}
}

func commonFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "capath", Value: "/ca_public"},
		&cli.StringFlag{Name: "datapath", Value: "/data/persistent"},
		&cli.StringFlag{Name: "tokenpath", Value: "token.jwt"},
	}
}

func main() {
	app := &cli.App{
		Name:  "kc_client_init",
		Usage: "Keycloak OIDC client management",
		Commands: []*cli.Command{
			{
				Name:   "get_jwt",
				Usage:  "Fetch a Keycloak Initial Access Token (IAT)",
				Action: getKCTokenAction,
				Flags:  commonFlags(),
			},
			{
				Name:   "register_oidc",
				Usage:  "Register the OIDC client using the stored token",
				Action: registerClientAction,
				Flags:  commonFlags(),
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func getKCTokenAction(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		return cli.Exit("Manifest path required", 1)
	}

	manifestPath := ctx.Args().Get(0)
	jsondata, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	rmBase, err := jsonparser.GetString(jsondata, "rasenmaeher", "mtls", "base_uri")
	if err != nil {
		return fmt.Errorf("could not find mtls base_uri in manifest: %w", err)
	}

	datapath := ctx.String("datapath")
	certpath := filepath.Join(datapath, "public", "mtlsclient.pem")
	keypath := filepath.Join(datapath, "private", "mtlsclient.key")

	if !fileExist(certpath) || !fileExist(keypath) {
		return errors.New("mtls certificates not found")
	}

	clientKP, err := tls.LoadX509KeyPair(certpath, keypath)
	if err != nil {
		return fmt.Errorf("failed to load keypair: %w", err)
	}

	certpool, _ := x509.SystemCertPool()
	if certpool == nil {
		certpool = x509.NewCertPool()
	}

	caFiles, _ := filepath.Glob(filepath.Join(ctx.String("capath"), "*.pem"))
	for _, f := range caFiles {
		raw, _ := os.ReadFile(f)
		certpool.AppendCertsFromPEM(raw)
	}

	client := resty.New()
	client.SetTLSClientConfig(&tls.Config{
		RootCAs:      certpool,
		Certificates: []tls.Certificate{clientKP},
	})

	url := fmt.Sprintf("https://%s/api/v1/product/kctoken", rmBase)
	log.Infof("Requesting token from %s", url)

	resp, err := client.R().
		SetResult(map[string]interface{}{}).
		Post(url)

	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("RASENMAEHER error (%d): %s", resp.StatusCode(), resp.Body())
	}
	token, err := jsonparser.GetString(resp.Body(), "token")
	if err != nil {
		return fmt.Errorf("could not find token in response: %s", resp.Body())
	}

	outputPath, err := safeJoin(datapath, "public", filepath.Base(ctx.String("tokenpath")))
	if err != nil {
		return fmt.Errorf("unsafe token output path: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(token), 0644); err != nil { // #nosec G703 -- path validated by safeJoin
		return fmt.Errorf("failed to write token file: %w", err)
	}

	log.Infof("Successfully saved token to %s", outputPath)
	fmt.Println(token)
	return nil
}

func registerClientAction(ctx *cli.Context) error {
	if ctx.Args().Len() < 1 {
		return cli.Exit("Manifest path required", 1)
	}

	manifestPath := ctx.Args().Get(0)
	jsondata, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	kcBase, err := jsonparser.GetString(jsondata, "rasenmaeher", "kc", "base_uri")
	kcRealm, err := jsonparser.GetString(jsondata, "rasenmaeher", "kc", "realm")

	datapath := ctx.String("datapath")
	tokenPath := filepath.Join(datapath, "public", ctx.String("tokenpath"))
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("could not read token file: %w", err)
	}

	clientName, err := jsonparser.GetString(jsondata, "oidc", "client_registration", "client_name")
	if err != nil {
		return fmt.Errorf("client_name is required in manifest: %w", err)
	}

	clientPayload := map[string]interface{}{
		"client_name":   clientName,
		"redirect_uris": []string{"*"},
	}
	certpool, _ := x509.SystemCertPool()
	if certpool == nil {
		certpool = x509.NewCertPool()
	}
	caFiles, _ := filepath.Glob(filepath.Join(ctx.String("capath"), "*.pem"))
	for _, f := range caFiles {
		raw, _ := os.ReadFile(f)
		certpool.AppendCertsFromPEM(raw)
	}
	client := resty.New()
	client.SetTLSClientConfig(&tls.Config{
		RootCAs: certpool,
	})

	url := fmt.Sprintf("https://%s/realms/%s/clients-registrations/openid-connect", kcBase, kcRealm)

	resp, err := client.R().
		SetHeader("Authorization", "Bearer "+string(token)).
		SetHeader("Content-Type", "application/json").
		SetBody(clientPayload).
		Post(url)

	if err != nil {
		return fmt.Errorf("registration request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("keycloak registration error (%d): %s", resp.StatusCode(), resp.Body())
	}

	secretPath, err := safeJoin(datapath, "oidc", "registration.json")
	if err != nil {
		return fmt.Errorf("unsafe secret output path: %w", err)
	}
	if err := os.WriteFile(secretPath, resp.Body(), 0600); err != nil { // #nosec G703 -- path validated by safeJoin
		return fmt.Errorf("failed to write registration secrets: %w", err)
	}

	log.Infof("Client registered successfully. Secrets saved to %s", secretPath)
	return nil
}
