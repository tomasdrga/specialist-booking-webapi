package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tomasdrga/specialist-booking-webapi/api"
	"github.com/tomasdrga/specialist-booking-webapi/internal/db_service"
	"github.com/tomasdrga/specialist-booking-webapi/internal/specialist_booking"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	log.Logger = zerolog.New(os.Stdout).With().
		Str("service", "specialist-booking-api").
		Timestamp().
		Caller().
		Logger()

	logLevelStr := os.Getenv("LOG_LEVEL")
	defaultLevel := zerolog.InfoLevel
	level, err := zerolog.ParseLevel(strings.ToLower(logLevelStr))
	if err != nil {
		log.Warn().Str("LOG_LEVEL", logLevelStr).Msgf("Invalid log level, using default: %s", defaultLevel)
		level = defaultLevel
	}
	zerolog.SetGlobalLevel(level)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	traceExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize trace exporter")
	}
	traceProvider := tracesdk.NewTracerProvider(tracesdk.WithBatcher(traceExporter))
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer traceProvider.Shutdown(ctx)

	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize metric reader")
	}
	metricProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(metricReader))
	otel.SetMeterProvider(metricProvider)
	defer metricProvider.Shutdown(ctx)

	log.Info().Msg("Specialist Booking API server started")
	port := os.Getenv("SPECIALIST_BOOKING_API_PORT")
	if port == "" {
		port = "8080"
	}
	environment := os.Getenv("SPECIALIST_BOOKING_API_ENVIRONMENT")
	if !strings.EqualFold(environment, "production") {
		gin.SetMode(gin.DebugMode)
	}

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(otelgin.Middleware("specialist-booking-webapi"))
	engine.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "PUT", "POST", "DELETE", "PATCH"},
		AllowHeaders:     []string{"Origin", "Authorization", "Content-Type"},
		ExposeHeaders:    []string{""},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	dbService := db_service.NewMongoService[specialist_booking.Clinic](db_service.MongoServiceConfig{})
	defer dbService.Disconnect(context.Background())
	engine.Use(func(ctx *gin.Context) {
		ctx.Set("db_service", dbService)
		ctx.Next()
	})

	specialist_booking.NewRouterWithGinEngine(engine, specialist_booking.ApiHandleFunctions{
		SpecialistBookingAPI: specialist_booking.NewBookingApi(),
	})

	engine.GET("/openapi", api.HandleOpenApi)
	if err := engine.Run(":" + port); err != nil {
		log.Fatal().Err(err).Msg("Specialist Booking API server stopped")
	}
}
