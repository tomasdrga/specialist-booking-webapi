package specialist_booking

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tomasdrga/specialist-booking-webapi/internal/db_service"
)

type memoryStore struct{ data map[string]*Clinic }

func newMemoryStore() *memoryStore { return &memoryStore{data: map[string]*Clinic{}} }
func (m *memoryStore) CreateDocument(_ context.Context, id string, document *Clinic) error {
	if _, ok := m.data[id]; ok {
		return db_service.ErrConflict
	}
	copy := *document
	m.data[id] = &copy
	return nil
}
func (m *memoryStore) FindDocument(_ context.Context, id string) (*Clinic, error) {
	if value, ok := m.data[id]; ok {
		copy := *value
		return &copy, nil
	}
	return nil, db_service.ErrNotFound
}
func (m *memoryStore) UpdateDocument(_ context.Context, id string, document *Clinic) error {
	if _, ok := m.data[id]; !ok {
		return db_service.ErrNotFound
	}
	copy := *document
	m.data[id] = &copy
	return nil
}
func (m *memoryStore) DeleteDocument(_ context.Context, id string) error {
	delete(m.data, id)
	return nil
}
func (m *memoryStore) Disconnect(_ context.Context) error { return nil }

func testRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	store := newMemoryStore()
	router.Use(func(ctx *gin.Context) { ctx.Set("db_service", ClinicStore(store)); ctx.Next() })
	NewRouterWithGinEngine(router, ApiHandleFunctions{SpecialistBookingAPI: NewBookingApi()})
	return router
}

func TestAppointmentsCrudAndValidation(t *testing.T) {
	router := testRouter()

	request := httptest.NewRequest(http.MethodGet, "/api/specialist-booking/specialist-clinic/appointments", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}

	invalidBody := bytes.NewReader([]byte(`{"id":"@new","patientId":"","patientName":"","startsAt":"2026-05-20T09:00:00Z","durationMinutes":0,"examinationType":"","status":"x"}`))
	request = httptest.NewRequest(http.MethodPost, "/api/specialist-booking/specialist-clinic/appointments", invalidBody)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid appointment, got %d", response.Code)
	}

	appointment := Appointment{Id: "@new", PatientId: "p-1", PatientName: "Test Patient", StartsAt: time.Now().Add(2 * time.Hour), DurationMinutes: 30, ExaminationType: "Kardiologické vyšetrenie", Status: "requested"}
	body, _ := json.Marshal(appointment)
	request = httptest.NewRequest(http.MethodPost, "/api/specialist-booking/specialist-clinic/appointments", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.Code)
	}

	var created Appointment
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Id == "" || created.Id == "@new" {
		t.Fatalf("expected generated id, got %q", created.Id)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/specialist-booking/specialist-clinic/appointments/"+created.Id, nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
}

func TestAssignBestSlotAndWaitingList(t *testing.T) {
	router := testRouter()
	clinic := "clinic-wait"

	appointment := Appointment{Id: "@new", PatientId: "p-2", PatientName: "Queue User", StartsAt: time.Now().Add(4 * time.Hour), DurationMinutes: 30, ExaminationType: "Imunológia", Status: "requested"}
	body, _ := json.Marshal(appointment)
	request := httptest.NewRequest(http.MethodPost, "/api/specialist-booking/"+clinic+"/appointments", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.Code)
	}
	var created Appointment
	_ = json.Unmarshal(response.Body.Bytes(), &created)

	request = httptest.NewRequest(http.MethodPost, "/api/specialist-booking/"+clinic+"/appointments/"+created.Id+"/assign-best-slot", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for waiting list, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/specialist-booking/"+clinic+"/waiting-list", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var waiting []WaitingListEntry
	_ = json.Unmarshal(response.Body.Bytes(), &waiting)
	if len(waiting) != 1 || waiting[0].AppointmentId != created.Id {
		t.Fatalf("expected waiting list with appointment %s", created.Id)
	}

	slot := TimeSlot{Id: "@new", StartsAt: time.Now().Add(5 * time.Hour), DurationMinutes: 30, Capacity: 1, Booked: 0, ExaminationType: "Imunológia", UrgentBlocked: false}
	slotBody, _ := json.Marshal(slot)
	request = httptest.NewRequest(http.MethodPost, "/api/specialist-booking/"+clinic+"/time-slots", bytes.NewReader(slotBody))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201 on slot create, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/specialist-booking/"+clinic+"/waiting-list", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var waitingAfter []WaitingListEntry
	_ = json.Unmarshal(response.Body.Bytes(), &waitingAfter)
	if len(waitingAfter) != 0 {
		t.Fatalf("expected waiting list to be empty after promotion, got %d", len(waitingAfter))
	}
}
