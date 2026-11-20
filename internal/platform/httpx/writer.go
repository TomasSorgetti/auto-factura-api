package httpx

import (
	"context"
	"net/http"
)

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) wroteHeader() bool { return w.status != 0 }

type requestInfo struct {
	pattern string
	code    string
}

type requestInfoKey struct{}

func withRequestInfo(ctx context.Context) (context.Context, *requestInfo) {
	info := &requestInfo{}
	return context.WithValue(ctx, requestInfoKey{}, info), info
}

func requestInfoFrom(ctx context.Context) *requestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(*requestInfo)
	return info
}
