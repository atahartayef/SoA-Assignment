package platform

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Correlate gives every request a correlation ID: the caller's if it sent one,
// a new one otherwise. It is echoed back so the client can quote it to support.
func Correlate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		id := request.Header.Get("X-Correlation-Id")
		if id == "" {
			id = uuid.NewString()
		}
		writer.Header().Set("X-Correlation-Id", id)
		next.ServeHTTP(writer, request.WithContext(WithCorrelation(request.Context(), id)))
	})
}

func JSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

// Health adds the two probes Kubernetes needs. Liveness answers while the process
// is running; readiness answers only while its backing services can be reached,
// so a service with a broken database is taken out of the load balancer rather
// than restarted in a loop.
func Health(router chi.Router, cfg Config, ready func(context.Context) error) {
	router.Get("/health/live", func(writer http.ResponseWriter, _ *http.Request) {
		JSON(writer, http.StatusOK, map[string]string{
			"status": "alive", "service": cfg.ServiceName, "version": cfg.Version})
	})
	router.Get("/health/ready", func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			JSON(writer, http.StatusServiceUnavailable, map[string]string{
				"status": "not-ready", "reason": err.Error()})
			return
		}
		JSON(writer, http.StatusOK, map[string]string{
			"status": "ready", "service": cfg.ServiceName, "version": cfg.Version})
	})
}

// SelfCheck is the container health check. The runtime image has no shell and no
// curl, so the binary checks itself: `service -healthcheck` exits 0 when ready.
func SelfCheck(port string) {
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/health/ready")
	if err != nil || response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	os.Exit(0)
}

// Serve runs the HTTP server and shuts it down cleanly on SIGTERM, so Kubernetes
// can replace a pod without dropping a request (12-factor IX).
func Serve(ctx context.Context, cfg Config, router http.Handler, logger *slog.Logger) error {
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("shutdown.started", slog.String("event", "shutdown.started"))
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("service.started", slog.String("event", "service.started"), slog.String("port", cfg.Port))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
