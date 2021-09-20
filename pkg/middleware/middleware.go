package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/weaveworks/weave-gitops/pkg/logger"
	"github.com/weaveworks/weave-gitops/pkg/services/auth"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/metadata"
)

type statusRecorder struct {
	http.ResponseWriter
	Status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
}

var RequestOkText = "request success"
var RequestErrorText = "request error"
var ServerErrorText = "server error"

// WithGrpcErrorLogging logs errors returned from server RPC handlers.
// Our errors happen in gRPC land, so we cannot introspect into the content of
// the error message in the WithLogging http.Handler.
// Normal gRPC middleware was not working for this:
// https://github.com/grpc-ecosystem/grpc-gateway/issues/1043
func WithGrpcErrorLogging(log logr.Logger) runtime.ServeMuxOption {
	return runtime.WithErrorHandler(func(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
		log.Error(err, ServerErrorText)
		// We don't want to change the behavior of error handling, just intercept for logging.
		runtime.DefaultHTTPErrorHandler(ctx, mux, marshaler, w, r, err)
	})
}

// WithLogging adds basic logging for HTTP requests.
// Note that this accepts a grpc-gateway ServeMux instead of an http.Handler.
func WithLogging(log logr.Logger, mux *runtime.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{
			ResponseWriter: w,
			Status:         200,
		}
		mux.ServeHTTP(recorder, r)

		l := log.WithValues("uri", r.RequestURI, "status", recorder.Status)

		if recorder.Status < 400 {
			l.V(logger.LogLevelDebug).Info(RequestOkText)
		}

		if recorder.Status >= 400 && recorder.Status < 500 {
			l.V(logger.LogLevelWarn).Info(RequestErrorText)
		}

		if recorder.Status >= 500 {
			l.V(logger.LogLevelError).Info(ServerErrorText)
		}
	})
}

type contextVals struct {
	ProviderToken *oauth2.Token
}

type key int

const tokenKey key = iota

// Injects the token into the request context to be retrieved later.
// Use the ExtractToken func inside the server handler where appropriate.
func WithProviderToken(jwtClient auth.JWTClient, h http.Handler, log logr.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.Header.Get("Authorization")
		tokenSlice := strings.Split(tokenStr, "token ")

		if len(tokenSlice) < 2 {
			// No token specified. Nothing to be done.
			// We do NOT return 400 here because there may be some 'unauthenticated' routes (ie /login)
			h.ServeHTTP(w, r)
			return
		}

		// The actual token data
		token := tokenSlice[1]

		claims, err := jwtClient.VerifyJWT(token)
		if err != nil {
			log.V(logger.LogLevelWarn).Info("could not parse claims")
			// Certain routes do not require a token, so pass the request through.
			// If the route requires a token and it isn't present,
			// the next handler will error and return that to the user.
			h.ServeHTTP(w, r)
			return
		}

		vals := contextVals{ProviderToken: &oauth2.Token{AccessToken: claims.ProviderToken}}

		c := context.WithValue(r.Context(), tokenKey, vals)
		r = r.WithContext(c)
		h.ServeHTTP(w, r)
	})
}

// Get the token from request context.
func ExtractProviderToken(ctx context.Context) (*oauth2.Token, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		return &oauth2.Token{AccessToken: md.Get("authorization")[0]}, nil
	}

	c := ctx.Value(tokenKey)

	vals, ok := c.(contextVals)
	if !ok {
		return nil, errors.New("could not get token from context")
	}

	if vals.ProviderToken == nil || vals.ProviderToken.AccessToken == "" {
		return nil, errors.New("no token specified")
	}

	return vals.ProviderToken, nil
}
