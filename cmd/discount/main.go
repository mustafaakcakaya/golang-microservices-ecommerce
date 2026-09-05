// Command discount runs the Discount gRPC service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/discountpb"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/grpcserver"
	"github.com/mustafaakcakaya/golang-microservices-ecommerce/internal/discount/sqlite"
)

const (
	defaultAddr         = ":8080"
	defaultDatabasePath = "discount.db"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("discount service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("DISCOUNT_GRPC_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	databasePath := os.Getenv("DISCOUNT_DATABASE_PATH")
	if databasePath == "" {
		databasePath = defaultDatabasePath
	}

	db, err := sqlite.Open(databasePath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := sqlite.Migrate(ctx, db); err != nil {
		return err
	}

	if os.Getenv("DISCOUNT_SEED") == "true" {
		seeded, err := sqlite.Seed(ctx, db)
		if err != nil {
			return err
		}
		log.Info("discount seeding finished", "inserted", seeded)
	}

	server := grpc.NewServer()
	discountpb.RegisterDiscountProtoServiceServer(server, grpcserver.New(sqlite.NewCouponRepository(db), log))

	// The standard health service replaces the HTTP /health endpoint the other
	// services expose: this process speaks only gRPC, and grpc_health_probe or
	// a Kubernetes gRPC probe can read it directly.
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Reflection lets grpcurl and similar tools explore the service without a
	// local copy of the .proto, which the .NET service gets from its Swagger UI.
	reflection.Register(server)

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("discount service listening", "addr", addr)
		serveErr <- server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("serving grpc: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Info("shutting down discount service")
		// GracefulStop lets in-flight calls finish before the process exits.
		server.GracefulStop()
		return nil
	}
}
