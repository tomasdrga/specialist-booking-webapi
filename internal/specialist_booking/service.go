package specialist_booking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tomasdrga/specialist-booking-webapi/internal/db_service"
)

type ClinicStore = db_service.DbService[Clinic]

func defaultClinic(id string) Clinic {
	now := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	return Clinic{
		Id: id,
		Appointments: []Appointment{
			{Id: "apt-001", PatientId: "p-10001", PatientName: "Jana Nováková", PatientEmail: "jana@example.com", ReferringDoctor: "MUDr. Kováč", StartsAt: now, DurationMinutes: 30, ExaminationType: "Kardiologické vyšetrenie", Status: "confirmed", Note: "Kontrola EKG"},
			{Id: "apt-002", PatientId: "p-10096", PatientName: "Peter Horváth", PatientEmail: "peter@example.com", StartsAt: now.Add(90 * time.Minute), DurationMinutes: 45, ExaminationType: "Neurologická konzultácia", Status: "requested"},
		},
		TimeSlots: []TimeSlot{
			{Id: "slot-001", StartsAt: now, DurationMinutes: 30, Capacity: 2, Booked: 1, ExaminationType: "Kardiologické vyšetrenie", UrgentBlocked: false},
			{Id: "slot-002", StartsAt: now.Add(90 * time.Minute), DurationMinutes: 45, Capacity: 1, Booked: 1, ExaminationType: "Neurologická konzultácia", UrgentBlocked: false},
			{Id: "slot-003", StartsAt: now.Add(150 * time.Minute), DurationMinutes: 30, Capacity: 1, Booked: 0, ExaminationType: "Dermatologická kontrola", UrgentBlocked: true},
		},
	}
}

func getOrCreateClinic(ctx context.Context, store ClinicStore, clinicId string) (*Clinic, error) {
	clinic, err := store.FindDocument(ctx, clinicId)
	if err == nil {
		return clinic, nil
	}
	if !errors.Is(err, db_service.ErrNotFound) {
		return nil, err
	}
	created := defaultClinic(clinicId)
	if err := store.CreateDocument(ctx, clinicId, &created); err != nil && !errors.Is(err, db_service.ErrConflict) {
		return nil, err
	}
	return &created, nil
}

func ensureId(id string) string {
	if id == "" || id == "@new" {
		return uuid.NewString()
	}
	return id
}
