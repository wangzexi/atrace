// atrace-server is the archive: the database, the web timeline and the HTTP API. It does not collect;
// collectors on the owner's computer write through that API.
package main

import (
	"bufio"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"git.zexi.me/zexi/atrace/server/auth"
	"git.zexi.me/zexi/atrace/server/core"
	"git.zexi.me/zexi/atrace/server/httpapi"
	"git.zexi.me/zexi/atrace/server/site"
)

func main() {
	dbPath := strings.TrimPrefix(env("ATRACE_DATABASE", "./data/atrace.db"), "sqlite:")
	files := strings.TrimPrefix(env("ATRACE_FILES", "./data/files"), "file:")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		log.Fatal(err)
	}
	store, err := core.Open(dbPath, files)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if len(os.Args) > 1 && os.Args[1] == "set-password" {
		setPassword(store)
		return
	}
	hub := httpapi.NewHub()
	addr := env("ATRACE_ADDR", "127.0.0.1:8183")
	mux := http.NewServeMux()
	owner := env("ATRACE_OWNER", "Alice")
	httpapi.RegisterRoutes(mux, store, hub, httpapi.Site{Owner: owner, DefaultTitle: owner + "'s Notes"})
	site.RegisterRoutes(mux, store, owner)
	login := auth.New(store)
	login.Register(mux)
	server := &http.Server{Addr: addr, Handler: login.Middleware(hub.Notify(mux)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 4 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("atrace-server listening at %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// setPassword sets the owner's password to the first line on standard input. It runs on the server, so the password
// is never sent over the network; use it to set the first password, or to reset one that is forgotten.
func setPassword(store *core.Store) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		log.Fatal(err)
	}
	password := strings.TrimRight(line, "\r\n")
	if err := auth.SetPassword(context.Background(), store, password); err != nil {
		log.Fatal(err)
	}
	log.Print("the owner's password is set")
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
