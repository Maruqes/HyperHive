package main

import "512SvMan/npm"

func main() {
	hostAdmin := "127.0.0.1:8181"
	base := "http://" + hostAdmin

	token, err := npm.SetupNPM(base)

	if err != nil {
		panic(err)
	}

	println("NPM setup complete, token:", token)

	proxyId, err := npm.CreateProxy(base, token, npm.Proxy{
		DomainNames:           []string{"test.localhost"},
		ForwardScheme:         "http",
		ForwardHost:           "127.0.0.1",
		ForwardPort:           8080,
		CachingEnabled:        false,
		BlockExploits:         true,
		AllowWebsocketUpgrade: true,
		AccessListID:          "0",
		CertificateID:         0,
		Meta:                  map[string]any{"letsencrypt_agree": false, "dns_challenge": false},
		AdvancedConfig:        "",
		Locations:             []any{},
		Http2Support:          false,
		HstsEnabled:           false,
		HstsSubdomains:        false,
		SslForced:             false,
	})
	if err != nil {
		panic(err)
	}

	err = npm.EditProxy(base, token, npm.Proxy{
		ID:                    proxyId,
		DomainNames:           []string{"test.localhost", "test2.localhost"},
		ForwardScheme:         "http",
		ForwardHost:           "127.0.0.1",
		ForwardPort:           8080,
		CachingEnabled:        false,
		BlockExploits:         true,
		AllowWebsocketUpgrade: true,
		AccessListID:          "0",
		CertificateID:         0,
		Meta:                  map[string]any{"letsencrypt_agree": false, "dns_challenge": false},
		AdvancedConfig:        "",
		Locations:             []any{},
		Http2Support:          false,
		HstsEnabled:           false,
		HstsSubdomains:        false,
		SslForced:             false,
	})
	if err != nil {
		panic(err)
	}
}
