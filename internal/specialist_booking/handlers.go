package specialist_booking

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

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

type ApiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AssignBestSlotResponse struct {
	AppointmentId string `json:"appointmentId"`
	Assigned      bool   `json:"assigned"`
	SlotId        string `json:"slotId,omitempty"`
	WaitingList   bool   `json:"waitingList"`
	Message       string `json:"message"`
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

func writeError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, ApiError{Code: code, Message: message})
}

func isValidAppointmentStatus(status string) bool {
	switch status {
	case "requested", "confirmed", "completed", "cancelled":
		return true
	default:
		return false
	}
}

func validateAppointment(appointment Appointment) error {
	if strings.TrimSpace(appointment.PatientId) == "" {
		return errors.New("patientId is required")
	}
	if strings.TrimSpace(appointment.PatientName) == "" {
		return errors.New("patientName is required")
	}
	if appointment.StartsAt.IsZero() {
		return errors.New("startsAt is required")
	}
	if appointment.DurationMinutes <= 0 {
		return errors.New("durationMinutes must be greater than 0")
	}
	if strings.TrimSpace(appointment.ExaminationType) == "" {
		return errors.New("examinationType is required")
	}
	if !isValidAppointmentStatus(appointment.Status) {
		return errors.New("status must be one of requested|confirmed|completed|cancelled")
	}
	return nil
}

func validateTimeSlot(slot TimeSlot) error {
	if slot.StartsAt.IsZero() {
		return errors.New("startsAt is required")
	}
	if slot.DurationMinutes <= 0 {
		return errors.New("durationMinutes must be greater than 0")
	}
	if slot.Capacity < 1 {
		return errors.New("capacity must be greater than or equal to 1")
	}
	if slot.Booked < 0 {
		return errors.New("booked must be greater than or equal to 0")
	}
	if slot.Booked > slot.Capacity {
		return errors.New("booked cannot exceed capacity")
	}
	if strings.TrimSpace(slot.ExaminationType) == "" {
		return errors.New("examinationType is required")
	}
	return nil
}

func findAppointmentIndex(clinic *Clinic, appointmentId string) int {
	for index := range clinic.Appointments {
		if clinic.Appointments[index].Id == appointmentId {
			return index
		}
	}
	return -1
}

func findSlotIndex(clinic *Clinic, slotId string) int {
	for index := range clinic.TimeSlots {
		if clinic.TimeSlots[index].Id == slotId {
			return index
		}
	}
	return -1
}

func removeWaitingListEntry(clinic *Clinic, appointmentId string) {
	filtered := make([]WaitingListEntry, 0, len(clinic.WaitingList))
	for _, entry := range clinic.WaitingList {
		if entry.AppointmentId != appointmentId {
			filtered = append(filtered, entry)
		}
	}
	clinic.WaitingList = filtered
}

func ensureWaitingListEntry(clinic *Clinic, appointment Appointment) {
	for _, entry := range clinic.WaitingList {
		if entry.AppointmentId == appointment.Id {
			return
		}
	}
	clinic.WaitingList = append(clinic.WaitingList, WaitingListEntry{
		AppointmentId:   appointment.Id,
		PatientName:     appointment.PatientName,
		ExaminationType: appointment.ExaminationType,
		RequestedAt:     time.Now().UTC(),
	})
}

func assignBestSlot(clinic *Clinic, appointmentIdx int) (slotId string, assigned bool) {
	appointment := clinic.Appointments[appointmentIdx]
	candidateIndexes := make([]int, 0)
	for i, slot := range clinic.TimeSlots {
		if slot.UrgentBlocked {
			continue
		}
		if slot.Booked >= slot.Capacity {
			continue
		}
		if slot.ExaminationType != appointment.ExaminationType {
			continue
		}
		candidateIndexes = append(candidateIndexes, i)
	}
	if len(candidateIndexes) == 0 {
		return "", false
	}
	sort.Slice(candidateIndexes, func(i, j int) bool {
		return clinic.TimeSlots[candidateIndexes[i]].StartsAt.Before(clinic.TimeSlots[candidateIndexes[j]].StartsAt)
	})
	best := candidateIndexes[0]
	clinic.TimeSlots[best].Booked++
	clinic.Appointments[appointmentIdx].AssignedSlotId = clinic.TimeSlots[best].Id
	if clinic.Appointments[appointmentIdx].Status == "requested" {
		clinic.Appointments[appointmentIdx].Status = "confirmed"
	}
	removeWaitingListEntry(clinic, clinic.Appointments[appointmentIdx].Id)
	return clinic.TimeSlots[best].Id, true
}

func promoteWaitingList(clinic *Clinic) {
	if len(clinic.WaitingList) == 0 {
		return
	}
	remaining := make([]WaitingListEntry, 0, len(clinic.WaitingList))
	for _, entry := range clinic.WaitingList {
		idx := findAppointmentIndex(clinic, entry.AppointmentId)
		if idx == -1 {
			continue
		}
		if clinic.Appointments[idx].Status == "cancelled" || clinic.Appointments[idx].Status == "completed" {
			continue
		}
		if _, assigned := assignBestSlot(clinic, idx); !assigned {
			remaining = append(remaining, entry)
		}
	}
	clinic.WaitingList = remaining
}

func (a *BookingApi) GetAppointments(c *gin.Context) {
	c, span, logger := a.requestContext(c, "GetAppointments")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic appointments")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa načítať objednávky")
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
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa načítať objednávku")
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
	writeError(c, http.StatusNotFound, "NOT_FOUND", "Objednávka neexistuje")
}

func (a *BookingApi) CreateAppointment(c *gin.Context) {
	c, span, logger := a.requestContext(c, "CreateAppointment")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before creating appointment")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa vytvoriť objednávku")
		return
	}
	var appointment Appointment
	if err := c.BindJSON(&appointment); err != nil {
		logger.Error().Err(err).Msg("Failed to bind appointment JSON")
		span.SetStatus(codes.Error, "Failed to bind appointment JSON")
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "Neplatný JSON požiadavky")
		return
	}
	appointment.Id = ensureId(appointment.Id)
	if findAppointmentIndex(clinic, appointment.Id) != -1 {
		writeError(c, http.StatusConflict, "CONFLICT", "Objednávka s daným ID už existuje")
		return
	}
	if err := validateAppointment(appointment); err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	span.SetAttributes(attribute.String("appointment.id", appointment.Id))
	appointment.Status = "requested"
	appointment.AssignedSlotId = ""
	clinic.Appointments = append(clinic.Appointments, appointment)
	appointmentIndex := len(clinic.Appointments) - 1
	if _, assigned := assignBestSlot(clinic, appointmentIndex); !assigned {
		ensureWaitingListEntry(clinic, clinic.Appointments[appointmentIndex])
	}
	appointment = clinic.Appointments[appointmentIndex]
	if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
		logger.Error().Err(err).Str("appointmentId", appointment.Id).Msg("Failed to store created appointment")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa uložiť objednávku")
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
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa upraviť objednávku")
		return
	}
	var appointment Appointment
	if err := c.BindJSON(&appointment); err != nil {
		logger.Error().Err(err).Msg("Failed to bind appointment JSON")
		span.SetStatus(codes.Error, "Failed to bind appointment JSON")
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "Neplatný JSON požiadavky")
		return
	}
	appointment.Id = appointmentId
	if err := validateAppointment(appointment); err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	for index := range clinic.Appointments {
		if clinic.Appointments[index].Id == appointment.Id {
			appointment.AssignedSlotId = clinic.Appointments[index].AssignedSlotId
			clinic.Appointments[index] = appointment
			if appointment.Status == "cancelled" {
				removeWaitingListEntry(clinic, appointment.Id)
			}
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				logger.Error().Err(err).Str("appointmentId", appointment.Id).Msg("Failed to store updated appointment")
				span.SetStatus(codes.Error, err.Error())
				writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa uložiť objednávku")
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
	writeError(c, http.StatusNotFound, "NOT_FOUND", "Objednávka neexistuje")
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
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa vymazať objednávku")
		return
	}
	for index := range clinic.Appointments {
		if clinic.Appointments[index].Id == appointmentId {
			if clinic.Appointments[index].AssignedSlotId != "" {
				slotIndex := findSlotIndex(clinic, clinic.Appointments[index].AssignedSlotId)
				if slotIndex != -1 && clinic.TimeSlots[slotIndex].Booked > 0 {
					clinic.TimeSlots[slotIndex].Booked--
				}
			}
			clinic.Appointments = append(clinic.Appointments[:index], clinic.Appointments[index+1:]...)
			removeWaitingListEntry(clinic, appointmentId)
			promoteWaitingList(clinic)
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				logger.Error().Err(err).Str("appointmentId", appointmentId).Msg("Failed to store deleted appointment")
				span.SetStatus(codes.Error, err.Error())
				writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa vymazať objednávku")
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
	writeError(c, http.StatusNotFound, "NOT_FOUND", "Objednávka neexistuje")
}

func (a *BookingApi) AssignBestSlot(c *gin.Context) {
	c, span, logger := a.requestContext(c, "AssignBestSlot")
	defer span.End()
	appointmentId := c.Param("appointmentId")
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before assignment")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa priradiť termín")
		return
	}
	appointmentIndex := findAppointmentIndex(clinic, appointmentId)
	if appointmentIndex == -1 {
		writeError(c, http.StatusNotFound, "NOT_FOUND", "Objednávka neexistuje")
		return
	}
	appointment := clinic.Appointments[appointmentIndex]
	if appointment.Status == "cancelled" || appointment.Status == "completed" {
		writeError(c, http.StatusConflict, "INVALID_STATE", "Objednávku v tomto stave nemožno priradiť")
		return
	}
	if appointment.AssignedSlotId != "" {
		c.JSON(http.StatusOK, AssignBestSlotResponse{AppointmentId: appointment.Id, Assigned: true, SlotId: appointment.AssignedSlotId, WaitingList: false, Message: "Objednávka už má priradený termín"})
		return
	}
	slotId, assigned := assignBestSlot(clinic, appointmentIndex)
	status := http.StatusOK
	response := AssignBestSlotResponse{AppointmentId: appointment.Id, Assigned: assigned, SlotId: slotId, WaitingList: false, Message: "Objednávke bol priradený najbližší voľný termín"}
	if !assigned {
		ensureWaitingListEntry(clinic, clinic.Appointments[appointmentIndex])
		status = http.StatusAccepted
		response.WaitingList = true
		response.Message = "Nenašiel sa voľný termín, objednávka bola zaradená do čakacej listiny"
	}
	if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
		logger.Error().Err(err).Str("appointmentId", appointment.Id).Msg("Failed to persist assignment")
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa uložiť priradenie")
		return
	}
	c.JSON(status, response)
}

func (a *BookingApi) GetWaitingList(c *gin.Context) {
	c, span, logger := a.requestContext(c, "GetWaitingList")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load waiting list")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa načítať čakaciu listinu")
		return
	}
	if clinic.WaitingList == nil {
		clinic.WaitingList = []WaitingListEntry{}
	}
	c.JSON(http.StatusOK, clinic.WaitingList)
}

func (a *BookingApi) GetTimeSlots(c *gin.Context) {
	c, span, logger := a.requestContext(c, "GetTimeSlots")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load time slots")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa načítať termíny")
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
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa načítať termín")
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
	writeError(c, http.StatusNotFound, "NOT_FOUND", "Termín neexistuje")
}

func (a *BookingApi) CreateTimeSlot(c *gin.Context) {
	c, span, logger := a.requestContext(c, "CreateTimeSlot")
	defer span.End()
	clinic, err := getOrCreateClinic(c.Request.Context(), storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load clinic before creating time slot")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa vytvoriť termín")
		return
	}
	var slot TimeSlot
	if err := c.BindJSON(&slot); err != nil {
		logger.Error().Err(err).Msg("Failed to bind time slot JSON")
		span.SetStatus(codes.Error, "Failed to bind time slot JSON")
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "Neplatný JSON požiadavky")
		return
	}
	slot.Id = ensureId(slot.Id)
	if findSlotIndex(clinic, slot.Id) != -1 {
		writeError(c, http.StatusConflict, "CONFLICT", "Termín s daným ID už existuje")
		return
	}
	if err := validateTimeSlot(slot); err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	span.SetAttributes(attribute.String("time_slot.id", slot.Id))
	clinic.TimeSlots = append(clinic.TimeSlots, slot)
	promoteWaitingList(clinic)
	if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
		logger.Error().Err(err).Str("slotId", slot.Id).Msg("Failed to store created time slot")
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa uložiť termín")
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
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa upraviť termín")
		return
	}
	var slot TimeSlot
	if err := c.BindJSON(&slot); err != nil {
		logger.Error().Err(err).Msg("Failed to bind time slot JSON")
		span.SetStatus(codes.Error, "Failed to bind time slot JSON")
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "Neplatný JSON požiadavky")
		return
	}
	slot.Id = slotId
	if err := validateTimeSlot(slot); err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	for index := range clinic.TimeSlots {
		if clinic.TimeSlots[index].Id == slot.Id {
			clinic.TimeSlots[index] = slot
			promoteWaitingList(clinic)
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				logger.Error().Err(err).Str("slotId", slot.Id).Msg("Failed to store updated time slot")
				span.SetStatus(codes.Error, err.Error())
				writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa uložiť termín")
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
	writeError(c, http.StatusNotFound, "NOT_FOUND", "Termín neexistuje")
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
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa vymazať termín")
		return
	}
	for index := range clinic.TimeSlots {
		if clinic.TimeSlots[index].Id == slotId {
			clinic.TimeSlots = append(clinic.TimeSlots[:index], clinic.TimeSlots[index+1:]...)
			for i := range clinic.Appointments {
				if clinic.Appointments[i].AssignedSlotId == slotId {
					clinic.Appointments[i].AssignedSlotId = ""
					if clinic.Appointments[i].Status == "confirmed" {
						clinic.Appointments[i].Status = "requested"
					}
					ensureWaitingListEntry(clinic, clinic.Appointments[i])
				}
			}
			promoteWaitingList(clinic)
			if err := storeFromContext(c).UpdateDocument(c.Request.Context(), clinic.Id, clinic); err != nil {
				if errors.Is(err, db_service.ErrNotFound) {
					span.SetStatus(codes.Error, "Clinic not found while deleting time slot")
					writeError(c, http.StatusNotFound, "NOT_FOUND", "Ambulancia neexistuje")
					return
				}
				logger.Error().Err(err).Str("slotId", slotId).Msg("Failed to store deleted time slot")
				span.SetStatus(codes.Error, err.Error())
				writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Nepodarilo sa vymazať termín")
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
	writeError(c, http.StatusNotFound, "NOT_FOUND", "Termín neexistuje")
}
