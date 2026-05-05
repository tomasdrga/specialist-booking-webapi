package specialist_booking

import "time"

type Appointment struct {
	Id              string    `json:"id" bson:"id"`
	PatientId       string    `json:"patientId" bson:"patientId"`
	PatientName     string    `json:"patientName" bson:"patientName"`
	PatientEmail    string    `json:"patientEmail,omitempty" bson:"patientEmail,omitempty"`
	ReferringDoctor string    `json:"referringDoctor,omitempty" bson:"referringDoctor,omitempty"`
	StartsAt        time.Time `json:"startsAt" bson:"startsAt"`
	DurationMinutes int32     `json:"durationMinutes" bson:"durationMinutes"`
	ExaminationType string    `json:"examinationType" bson:"examinationType"`
	Status          string    `json:"status" bson:"status"`
	Note            string    `json:"note,omitempty" bson:"note,omitempty"`
}

type TimeSlot struct {
	Id              string    `json:"id" bson:"id"`
	StartsAt        time.Time `json:"startsAt" bson:"startsAt"`
	DurationMinutes int32     `json:"durationMinutes" bson:"durationMinutes"`
	Capacity        int32     `json:"capacity" bson:"capacity"`
	Booked          int32     `json:"booked" bson:"booked"`
	ExaminationType string    `json:"examinationType" bson:"examinationType"`
	UrgentBlocked   bool      `json:"urgentBlocked" bson:"urgentBlocked"`
}

type Clinic struct {
	Id           string        `json:"id" bson:"id"`
	Appointments []Appointment `json:"appointments" bson:"appointments"`
	TimeSlots    []TimeSlot    `json:"timeSlots" bson:"timeSlots"`
}
