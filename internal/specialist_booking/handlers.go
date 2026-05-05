package specialist_booking

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tomasdrga/specialist-booking-webapi/internal/db_service"
)

func storeFromContext(c *gin.Context) ClinicStore {
	return c.MustGet("db_service").(ClinicStore)
}

type BookingApi struct{}

func NewBookingApi() *BookingApi { return &BookingApi{} }

func (a *BookingApi) GetAppointments(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, clinic.Appointments)
}

func (a *BookingApi) GetAppointment(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for _, appointment := range clinic.Appointments {
		if appointment.Id == c.Param("appointmentId") {
			c.JSON(http.StatusOK, appointment)
			return
		}
	}
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) CreateAppointment(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var appointment Appointment
	if err := c.BindJSON(&appointment); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	appointment.Id = ensureId(appointment.Id)
	clinic.Appointments = append(clinic.Appointments, appointment)
	if err := storeFromContext(c).UpdateDocument(c, clinic.Id, clinic); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, appointment)
}

func (a *BookingApi) UpdateAppointment(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var appointment Appointment
	if err := c.BindJSON(&appointment); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	appointment.Id = c.Param("appointmentId")
	for index := range clinic.Appointments {
		if clinic.Appointments[index].Id == appointment.Id {
			clinic.Appointments[index] = appointment
			if err := storeFromContext(c).UpdateDocument(c, clinic.Id, clinic); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			c.JSON(http.StatusOK, appointment)
			return
		}
	}
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) DeleteAppointment(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for index := range clinic.Appointments {
		if clinic.Appointments[index].Id == c.Param("appointmentId") {
			clinic.Appointments = append(clinic.Appointments[:index], clinic.Appointments[index+1:]...)
			if err := storeFromContext(c).UpdateDocument(c, clinic.Id, clinic); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			c.Status(http.StatusNoContent)
			return
		}
	}
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) GetTimeSlots(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, clinic.TimeSlots)
}

func (a *BookingApi) GetTimeSlot(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for _, slot := range clinic.TimeSlots {
		if slot.Id == c.Param("slotId") {
			c.JSON(http.StatusOK, slot)
			return
		}
	}
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) CreateTimeSlot(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var slot TimeSlot
	if err := c.BindJSON(&slot); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	slot.Id = ensureId(slot.Id)
	clinic.TimeSlots = append(clinic.TimeSlots, slot)
	if err := storeFromContext(c).UpdateDocument(c, clinic.Id, clinic); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, slot)
}

func (a *BookingApi) UpdateTimeSlot(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var slot TimeSlot
	if err := c.BindJSON(&slot); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	slot.Id = c.Param("slotId")
	for index := range clinic.TimeSlots {
		if clinic.TimeSlots[index].Id == slot.Id {
			clinic.TimeSlots[index] = slot
			if err := storeFromContext(c).UpdateDocument(c, clinic.Id, clinic); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			c.JSON(http.StatusOK, slot)
			return
		}
	}
	c.Status(http.StatusNotFound)
}

func (a *BookingApi) DeleteTimeSlot(c *gin.Context) {
	clinic, err := getOrCreateClinic(c, storeFromContext(c), c.Param("clinicId"))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for index := range clinic.TimeSlots {
		if clinic.TimeSlots[index].Id == c.Param("slotId") {
			clinic.TimeSlots = append(clinic.TimeSlots[:index], clinic.TimeSlots[index+1:]...)
			if err := storeFromContext(c).UpdateDocument(c, clinic.Id, clinic); err != nil {
				if errors.Is(err, db_service.ErrNotFound) {
					c.Status(http.StatusNotFound)
					return
				}
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			c.Status(http.StatusNoContent)
			return
		}
	}
	c.Status(http.StatusNotFound)
}
