package specialist_booking

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestAppointmentsCrud(t *testing.T) {
	router := testRouter()

	request := httptest.NewRequest(http.MethodGet, "/api/specialist-booking/specialist-clinic/appointments", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}

	appointment := Appointment{Id: "@new", PatientId: "p-1", PatientName: "Test Patient", DurationMinutes: 30, ExaminationType: "Kardiológia", Status: "requested"}
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
