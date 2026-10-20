package npm

import (
	"context"
	"strings"
)

type upstreamNPMContextKey struct{}
type upstreamNPMCredentials struct{ Base, Token string }

func WithUpstreamNPMContext(ctx context.Context, base, token string) context.Context {
	return context.WithValue(ctx, upstreamNPMContextKey{}, upstreamNPMCredentials{Base: base, Token: token})
}

func verifyUpstreamReferences(ctx context.Context, state upstreamSnapshot) error {
	if len(state.Bindings) == 0 {
		return nil
	}
	credentials, ok := ctx.Value(upstreamNPMContextKey{}).(upstreamNPMCredentials)
	if !ok {
		token, err := ServiceToken(ctx, "npm-data")
		if err != nil {
			return upstreamError(503, "upstream_reference_verification_unavailable")
		}
		credentials = upstreamNPMCredentials{Base: "http://127.0.0.1:81", Token: token}
	}
	proxies, err := rawGetAllProxys(credentials.Base, credentials.Token)
	if err != nil {
		return upstreamError(503, "upstream_reference_verification_unavailable")
	}
	streams, err := rawListStreams(credentials.Base, credentials.Token)
	if err != nil {
		return upstreamError(503, "upstream_reference_verification_unavailable")
	}
	actual := map[string]map[int]upstreamConsumer{"proxy": {}, "stream": {}}
	bound := map[string]map[int]map[string]bool{"proxy": {}, "stream": {}}
	for i := range proxies {
		p := &proxies[i]
		actual["proxy"][p.ID] = upstreamConsumer{Kind: "proxy", Proxy: p}
	}
	for i := range streams {
		p := &streams[i]
		actual["stream"][p.ID] = upstreamConsumer{Kind: "stream", Stream: p}
	}
	for _, b := range state.Bindings {
		kind := b.Consumer
		if kind == "location" {
			kind = "proxy"
		}
		c, ok := actual[kind][b.ID]
		if !ok || !consumerMatches(b.Expected, c) {
			return upstreamError(409, "upstream_consumer_drift")
		}
		if bound[kind][b.ID] == nil {
			bound[kind][b.ID] = map[string]bool{}
		}
		bound[kind][b.ID][b.Path] = true
	}
	for _, p := range proxies {
		if strings.HasPrefix(p.ForwardHost, "hh_upstream_") && !bound["proxy"][p.ID][""] {
			return upstreamError(409, "untracked_upstream_reference")
		}
		for _, l := range p.Locations {
			if strings.HasPrefix(l.ForwardHost, "hh_upstream_") && !bound["proxy"][p.ID][l.Path] {
				return upstreamError(409, "untracked_upstream_reference")
			}
		}
	}
	for _, p := range streams {
		if strings.HasPrefix(p.Forwarding_host, "hh_upstream_") && !bound["stream"][p.ID][""] {
			return upstreamError(409, "untracked_upstream_reference")
		}
	}
	return nil
}
