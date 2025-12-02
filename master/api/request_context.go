package api

import (
	"context"
	"net/http"
)

// keepAliveCtx keeps request values/deadlines but ignores client disconnects so long-running actions finish.
func keepAliveCtx(r *http.Request) context.Context {
	if r == nil {
		return context.Background()
	}
	return context.WithoutCancel(r.Context())
}
