package specialist_booking

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tomasdrga/specialist-booking-webapi/internal/db_service"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func storeFromContext(c *gin.Context) ClinicStore {
	return c.MustGet("db_service").(ClinicStore)
}

type BookingApi struct {
	logger                     zerolog.Logger
	tracer                     trace.Tracer
	appointmentsCreatedCounter metric.Int64Counter
	appointmentsUpdatedCounter metric.Int64Counter
	appointmentsDeletedCounter metric.Int64Counter
	timeSlotsCreatedCounter    metric.Int64Counter
	timeSlotsUpdatedCounter    metric.Int64Counter
	timeSlotsDeletedCounter    metric.Int64Counter
}

func mustCounter(meter metric.Meter, name string, description string) metric.Int64Counter {
	counter, err := meter.Int64Counter(name, metric.WithDescription(description))
	if err != nil {
		panic(err)
	}
	return counter
}

func NewBookingApi() *BookingApi {
	meter := otel.Meter("specialist-booking")
	return &BookingApi{
		logger:                     log.With().Str("component", "specialist-booking").Logger(),
		tracer:                     otel.Tracer("specialist-booking"),
		appointmentsCreatedCounter: mustCounter(meter, "specialist_booking_appointments_created_total", "Total number of appointments created"),
		appointmentsUpdatedCounter: mustCounter(meter, "specialist_booking_appointments_updated_total", "Total number of appointments updated"),
		appointmentsDeletedCounter: mustCounter(meter, "specialist_booking_appointments_deleted_total", "Total number of appointments deleted"),
		timeSlotsCreatedCounter:    mustCounter(meter, "specialist_booking_time_slots_created_total", "Total number of time slots created"),
		timeSlotsUpdatedCounter:    mustCounter(meter, "specialist_booking_time_slots_updated_total", "Total number of time slots updated"),
		timeSlotsDeletedCounter:    mustCounter(meter, "specialist_booking_time_slots_deleted_total", "Total number of time slots deleted"),
	}
}

func (a *BookingApi) requestContext(c *gin.Context, operation string) (*gin.Context, trace.Span, zerolog.Logger) {
	clinicId := c.Param("clinicId")
	ctx, span := a.tracer.Start(c.Request.Context(), operation, trace.WithAttributes(attribute.String("clinic.id", clinicId)))
	c.Request = c.Request.WithContext(ctx)
	logger := a.logger.With().Str("method", operation).Str("clinicId", clinicId).Logger()
	return c, span, logger
}

func metricAttributes(clinic *Clinic) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("clinic_id", clinic.Id))
}

func (a *BookingApi) GetAppointments(c *gin.Context) {
	c, span, logger := a.requestContext(c, "GetAppointments")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic appointments")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	span.SetStatus(codes.Ok, "Appointments loaded")
	c.JSON(http.StatusOK, clinic.Appointments)
}

func (a *BookingApi) GetAppointment(c *gin.Context) {
	c, span, logger := a.requestContext(c, "GetAppointment")
	defer span.End()
	appointmentId := c.Param("appointmentId")
	span.SetAttributes(attribute.String("appointment.id", appointmentId))
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic appointment")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for _, appointment := range clinic.Appointments {
		if appointment.Id == appointmentId {
			span.SetStatus(codes.Ok, "Appointment found")
			c.JSON(http.StatusOK, appointment)
			return
		}
	}
	span.SetStatus(codes.Error, "Appointment not found")
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) CreateAppointment(c *gin.Context) {
	c, span, logger := a.requestContext(c, "CreateAppointment")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before creating appointment")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var appointment Appointment
	if err := c.BindJSON(&appointment); err != nil {
		logger.Error().Err(err).Msg("Failed to bind appointment JSON")
		span.SetStatus(codes.Error, "Failed to bind appointment JSON")
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	appointment.Id = ensureId(appointment.Id)
	span.SetAttributes(attribute.String("appointment.id", appointment.Id))
	clinic.Appointments = append(clinic.Appointments, appointment)
	if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
		logger.Error().Err(err).Str("appointmentId", appointment.Id).Msg("Failed to store created appointment")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	logger.Info().Str("appointmentId", appointment.Id).Msg("Successfully created appointment")
	span.SetStatus(codes.Ok, "Appointment created")
	a.appointmentsCreatedCounter.Add(c.Request.Context(), 1, metricAttributes(clinic))
	c.JSON(http.StatusCreated, appointment)
}

func (a *BookingApi) UpdateAppointment(c *gin.Context) {
	c, span, logger := a.requestContext(c, "UpdateAppointment")
	defer span.End()
	appointmentId := c.Param("appointmentId")
	span.SetAttributes(attribute.String("appointment.id", appointmentId))
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before updating appointment")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var appointment Appointment
	if err := c.BindJSON(&appointment); err != nil {
		logger.Error().Err(err).Msg("Failed to bind appointment JSON")
		span.SetStatus(codes.Error, "Failed to bind appointment JSON")
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	appointment.Id = appointmentId
	for index := range clinic.Appointments {
		if clinic.Appointments[index].Id == appointment.Id {
			clinic.Appointments[index] = appointment
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				logger.Error().Err(err).Str("appointmentId", appointment.Id).Msg("Failed to store updated appointment")
				span.SetStatus(codes.Error, err.Error())
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			logger.Info().Str("appointmentId", appointment.Id).Msg("Successfully updated appointment")
			span.SetStatus(codes.Ok, "Appointment updated")
			a.appointmentsUpdatedCounter.Add(c.Request.Context(), 1, metricAttributes(clinic))
			c.JSON(http.StatusOK, appointment)
			return
		}
	}
	span.SetStatus(codes.Error, "Appointment not found")
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) DeleteAppointment(c *gin.Context) {
	c, span, logger := a.requestContext(c, "DeleteAppointment")
	defer span.End()
	appointmentId := c.Param("appointmentId")
	span.SetAttributes(attribute.String("appointment.id", appointmentId))
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before deleting appointment")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for index := range clinic.Appointments {
		if clinic.Appointments[index].Id == appointmentId {
			clinic.Appointments = append(clinic.Appointments[:index], clinic.Appointments[index+1:]...)
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				logger.Error().Err(err).Str("appointmentId", appointmentId).Msg("Failed to store deleted appointment")
				span.SetStatus(codes.Error, err.Error())
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			logger.Info().Str("appointmentId", appointmentId).Msg("Successfully deleted appointment")
			span.SetStatus(codes.Ok, "Appointment deleted")
			a.appointmentsDeletedCounter.Add(c.Request.Context(), 1, metricAttributes(clinic))
			c.Status(http.StatusNoContent)
			return
		}
	}
	span.SetStatus(codes.Error, "Appointment not found")
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) GetTimeSlots(c *gin.Context) {
	c, span, logger := a.requestContext(c, "GetTimeSlots")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load time slots")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	span.SetStatus(codes.Ok, "Time slots loaded")
	c.JSON(http.StatusOK, clinic.TimeSlots)
}

func (a *BookingApi) GetTimeSlot(c *gin.Context) {
	c, span, logger := a.requestContext(c, "GetTimeSlot")
	defer span.End()
	slotId := c.Param("slotId")
	span.SetAttributes(attribute.String("time_slot.id", slotId))
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load time slot")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for _, slot := range clinic.TimeSlots {
		if slot.Id == slotId {
			span.SetStatus(codes.Ok, "Time slot found")
			c.JSON(http.StatusOK, slot)
			return
		}
	}
	span.SetStatus(codes.Error, "Time slot not found")
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) CreateTimeSlot(c *gin.Context) {
	c, span, logger := a.requestContext(c, "CreateTimeSlot")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before creating time slot")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var slot TimeSlot
	if err := c.BindJSON(&slot); err != nil {
		logger.Error().Err(err).Msg("Failed to bind time slot JSON")
		span.SetStatus(codes.Error, "Failed to bind time slot JSON")
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	slot.Id = ensureId(slot.Id)
	span.SetAttributes(attribute.String("time_slot.id", slot.Id))
	clinic.TimeSlots = append(clinic.TimeSlots, slot)
	if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
		logger.Error().Err(err).Str("slotId", slot.Id).Msg("Failed to store created time slot")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	logger.Info().Str("slotId", slot.Id).Msg("Successfully created time slot")
	span.SetStatus(codes.Ok, "Time slot created")
	a.timeSlotsCreatedCounter.Add(c.Request.Context(), 1, metricAttributes(clinic))
	c.JSON(http.StatusCreated, slot)
}

func (a *BookingApi) UpdateTimeSlot(c *gin.Context) {
	c, span, logger := a.requestContext(c, "UpdateTimeSlot")
	defer span.End()
	slotId := c.Param("slotId")
	span.SetAttributes(attribute.String("time_slot.id", slotId))
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before updating time slot")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var slot TimeSlot
	if err := c.BindJSON(&slot); err != nil {
		logger.Error().Err(err).Msg("Failed to bind time slot JSON")
		span.SetStatus(codes.Error, "Failed to bind time slot JSON")
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	slot.Id = slotId
	for index := range clinic.TimeSlots {
		if clinic.TimeSlots[index].Id == slot.Id {
			clinic.TimeSlots[index] = slot
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				logger.Error().Err(err).Str("slotId", slot.Id).Msg("Failed to store updated time slot")
				span.SetStatus(codes.Error, err.Error())
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			logger.Info().Str("slotId", slot.Id).Msg("Successfully updated time slot")
			span.SetStatus(codes.Ok, "Time slot updated")
			a.timeSlotsUpdatedCounter.Add(c.Request.Context(), 1, metricAttributes(clinic))
			c.JSON(http.StatusOK, slot)
			return
		}
	}
	span.SetStatus(codes.Error, "Time slot not found")
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) DeleteTimeSlot(c *gin.Context) {
	c, span, logger := a.requestContext(c, "DeleteTimeSlot")
	defer span.End()
	slotId := c.Param("slotId")
	span.SetAttributes(attribute.String("time_slot.id", slotId))
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before deleting time slot")
		span.SetStatus(codes.Error, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for index := range clinic.TimeSlots {
		if clinic.TimeSlots[index].Id == slotId {
			clinic.TimeSlots = append(clinic.TimeSlots[:index], clinic.TimeSlots[index+1:]...)
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				if errors.Is(err, db_service.ErrNotFound) {
					span.SetStatus(codes.Error, "Clinic not found while deleting time slot")
					c.Status(http.StatusNotFound)
					return
				}
				logger.Error().Err(err).Str("slotId", slotId).Msg("Failed to store deleted time slot")
				span.SetStatus(codes.Error, err.Error())
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			logger.Info().Str("slotId", slotId).Msg("Successfully deleted time slot")
			span.SetStatus(codes.Ok, "Time slot deleted")
			a.timeSlotsDeletedCounter.Add(c.Request.Context(), 1, metricAttributes(clinic))
			c.Status(http.StatusNoContent)
			return
		}
	}
	span.SetStatus(codes.Error, "Time slot not found")
	c.Status(http.StatusNotFound)
}
