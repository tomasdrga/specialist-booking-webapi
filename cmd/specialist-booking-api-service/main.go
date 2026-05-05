package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/tomasdrga/specialist-booking-webapi/api"
	"github.com/tomasdrga/specialist-booking-webapi/internal/db_service"
	"github.com/tomasdrga/specialist-booking-webapi/internal/specialist_booking"
)

func main() {
	log.Printf("Specialist Booking API server started")
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
	engine.Run(":" + port)
}
