// Command discount runs the Discount gRPC service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	// The container image carries no shell tools that speak gRPC, so the binary
	// doubles as its own health probe: `service -healthcheck` dials the local
	// server and exits non-zero unless it reports SERVING.
	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if *healthcheck {
		if err := probe(); err != nil {
			log.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(log); err != nil {
		log.Error("discount service stopped with error", "error", err)
		os.Exit(1)
	}
}

// probe checks the server this binary would serve, addressing it on localhost.
func probe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	addr := os.Getenv("DISCOUNT_GRPC_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	// A listen address such as ":8080" has no host; dial the loopback.
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dialling %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("status is %s", response.GetStatus())
	}

	return nil
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
	// local copy of the .proto.
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
