// Command labapp runs the practice target. Configuration comes from the
// environment so no secret is baked into the image.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gosumdochi/internal/labapp"
)

func need(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("labapp: %s is required", name)
	}
	return v
}

func main() {
	app, err := labapp.New(labapp.Config{
		Mode:   labapp.Mode(need("LAB_MODE")),
		Seed:   os.Getenv("LAB_SEED"),
		DBPath: os.Getenv("LAB_DB"),
		Users: []labapp.Credential{
			{UserID: "u-a", DisplayName: "Alice", Username: need("LAB_A_USER"), Password: need("LAB_A_PASS")},
			{UserID: "u-b", DisplayName: "Bob", Username: need("LAB_B_USER"), Password: need("LAB_B_PASS")},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	addr := os.Getenv("LAB_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	// Deliberately does not log the mode.
	fmt.Fprintln(os.Stderr, "labapp listening on", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
