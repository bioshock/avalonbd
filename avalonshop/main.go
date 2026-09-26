package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"avalonshop/internal/app"
	"avalonshop/internal/config"
	"avalonshop/internal/mail"
	"avalonshop/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the running server and exit 0 if healthy")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// probe is used as the Docker HEALTHCHECK; the distroless image has no curl.
func probe() int {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:8080/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// seedAdminOrWarn seeds the first admin account, but never treats a failure
// as fatal to the storefront (C3): a duplicate ADMIN_EMAIL already held by a
// non-admin user, or an over-long ADMIN_PASSWORD, used to return an error
// that main() logged as "fatal" and exited on — Coolify then restarts with
// the same env vars and fails again, forever. A storefront that is up
// without a seeded admin is strictly better than a site that is down, so
// this logs the problem and what to do about it, and lets the caller
// continue booting.
func seedAdminOrWarn(ctx context.Context, st *store.Store, log *slog.Logger, email, password string) {
	if err := st.SeedAdmin(ctx, email, password); err != nil {
		log.Error("seed admin failed; the storefront will keep serving without a newly seeded admin account — fix ADMIN_EMAIL/ADMIN_PASSWORD and redeploy, or promote an existing user to admin directly in the database", "err", err)
	}
}

func run(log *slog.Logger) error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool, migrationsFS); err != nil {
		return err
	}
	st := store.New(pool)
	seedAdminOrWarn(ctx, st, log, cfg.AdminEmail, cfg.AdminPassword)
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		return err
	}
	mailer, err := mail.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.MailFrom, templatesFS, log)
	if err != nil {
		return err
	}
	a, err := app.New(cfg, st, mailer, templatesFS, staticFS, log)
	if err != nil {
		return err
	}
	if err := a.LoadSettings(ctx); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			log.Error("shutdown", "err", err)
		}
		mailer.Wait()
	}()
	log.Info("listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-done
	return nil
}
