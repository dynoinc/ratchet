package github_integration

import (
	"net/http"
	"sync"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v53/github"
	"golang.org/x/oauth2"
)

type key struct {
	appID          int64
	installationID int64
	privateKeyPath string
	token          string
}

var (
	mu    sync.Mutex
	cache sync.Map
)

func ForToken(token string) (*github.Client, error) {
	k := key{token: token}
	if v, ok := cache.Load(k); ok {
		return v.(*github.Client), nil
	}

	mu.Lock()
	defer mu.Unlock()

	transport := &oauth2.Transport{
		Base: http.DefaultTransport,
		Source: oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		),
	}

	client := github.NewClient(&http.Client{Transport: transport})
	cache.Store(k, client)
	return client, nil
}

func ForApp(appID, installationID int64, privateKeyPath string) (*github.Client, error) {
	k := key{appID: appID, installationID: installationID, privateKeyPath: privateKeyPath}
	if v, ok := cache.Load(k); ok {
		return v.(*github.Client), nil
	}

	mu.Lock()
	defer mu.Unlock()

	transport, err := ghinstallation.NewKeyFromFile(
		http.DefaultTransport,
		appID,
		installationID,
		privateKeyPath,
	)
	if err != nil {
		return nil, err
	}

	r := github.NewClient(&http.Client{Transport: transport})
	cache.Store(k, r)
	return r, nil
}
