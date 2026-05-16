package specialist_booking

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Route struct {
	Name        string
	Method      string
	Pattern     string
	HandlerFunc gin.HandlerFunc
}

type ApiHandleFunctions struct {
	SpecialistBookingAPI *BookingApi
}

func NewRouterWithGinEngine(router *gin.Engine, handleFunctions ApiHandleFunctions) *gin.Engine {
	for _, route := range getRoutes(handleFunctions) {
		switch route.Method {
		case http.MethodGet:
			router.GET(route.Pattern, route.HandlerFunc)
		case http.MethodPost:
			router.POST(route.Pattern, route.HandlerFunc)
		case http.MethodPut:
			router.PUT(route.Pattern, route.HandlerFunc)
		case http.MethodDelete:
			router.DELETE(route.Pattern, route.HandlerFunc)
		}
	}
	return router
}

func getRoutes(handleFunctions ApiHandleFunctions) []Route {
	api := handleFunctions.SpecialistBookingAPI
	return []Route{
		{"GetAppointments", http.MethodGet, "/api/specialist-booking/:clinicId/appointments", api.GetAppointments},
		{"CreateAppointment", http.MethodPost, "/api/specialist-booking/:clinicId/appointments", api.CreateAppointment},
		{"GetAppointment", http.MethodGet, "/api/specialist-booking/:clinicId/appointments/:appointmentId", api.GetAppointment},
		{"UpdateAppointment", http.MethodPut, "/api/specialist-booking/:clinicId/appointments/:appointmentId", api.UpdateAppointment},
		{"DeleteAppointment", http.MethodDelete, "/api/specialist-booking/:clinicId/appointments/:appointmentId", api.DeleteAppointment},
		{"AssignBestSlot", http.MethodPost, "/api/specialist-booking/:clinicId/appointments/:appointmentId/assign-best-slot", api.AssignBestSlot},
		{"GetWaitingList", http.MethodGet, "/api/specialist-booking/:clinicId/waiting-list", api.GetWaitingList},
		{"GetTimeSlots", http.MethodGet, "/api/specialist-booking/:clinicId/time-slots", api.GetTimeSlots},
		{"CreateTimeSlot", http.MethodPost, "/api/specialist-booking/:clinicId/time-slots", api.CreateTimeSlot},
		{"GetTimeSlot", http.MethodGet, "/api/specialist-booking/:clinicId/time-slots/:slotId", api.GetTimeSlot},
		{"UpdateTimeSlot", http.MethodPut, "/api/specialist-booking/:clinicId/time-slots/:slotId", api.UpdateTimeSlot},
		{"DeleteTimeSlot", http.MethodDelete, "/api/specialist-booking/:clinicId/time-slots/:slotId", api.DeleteTimeSlot},
	}
}
