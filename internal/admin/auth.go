package admin

import (
	"context"
	"net/http"
)

// Permission 是后续权限/白名单体系的接入点。本期不实现权限逻辑，接口与调用链先留好。
type Permission interface {
	Allow(ctx context.Context, r *http.Request) bool
}

// AllowAll 是本期使用的实现：直接放行。
type AllowAll struct{}

func (AllowAll) Allow(context.Context, *http.Request) bool { return true }

// WithAuth 把 Permission 串到 handler 前，本期等价于直接放行。
func WithAuth(p Permission, next http.Handler) http.Handler {
	if p == nil {
		p = AllowAll{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.Allow(r.Context(), r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
