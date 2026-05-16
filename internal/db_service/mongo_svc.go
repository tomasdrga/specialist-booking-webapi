package db_service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type DbService[DocType interface{}] interface {
	CreateDocument(ctx context.Context, id string, document *DocType) error
	FindDocument(ctx context.Context, id string) (*DocType, error)
	UpdateDocument(ctx context.Context, id string, document *DocType) error
	DeleteDocument(ctx context.Context, id string) error
	Disconnect(ctx context.Context) error
}

var ErrNotFound = fmt.Errorf("document not found")
var ErrConflict = fmt.Errorf("conflict: document already exists")

type MongoServiceConfig struct {
	ServerHost string
	ServerPort int
	UserName   string
	Password   string
	DbName     string
	Collection string
	Timeout    time.Duration
}

type mongoSvc[DocType interface{}] struct {
	MongoServiceConfig
	client     atomic.Pointer[mongo.Client]
	clientLock sync.Mutex
	tracer     trace.Tracer
}

func NewMongoService[DocType interface{}](config MongoServiceConfig) DbService[DocType] {
	enviro := func(name string, defaultValue string) string {
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		return defaultValue
	}

	svc := &mongoSvc[DocType]{}
	svc.tracer = otel.Tracer("MongoService")
	svc.MongoServiceConfig = config
	if svc.ServerHost == "" {
		svc.ServerHost = enviro("SPECIALIST_BOOKING_API_MONGODB_HOST", "localhost")
	}
	if svc.ServerPort == 0 {
		port := enviro("SPECIALIST_BOOKING_API_MONGODB_PORT", "27017")
		if parsed, err := strconv.Atoi(port); err == nil {
			svc.ServerPort = parsed
		} else {
			log.Warn().Str("port", port).Msg("Invalid MongoDB port, using default")
			svc.ServerPort = 27017
		}
	}
	if svc.UserName == "" {
		svc.UserName = enviro("SPECIALIST_BOOKING_API_MONGODB_USERNAME", "")
	}
	if svc.Password == "" {
		svc.Password = enviro("SPECIALIST_BOOKING_API_MONGODB_PASSWORD", "")
	}
	if svc.DbName == "" {
		svc.DbName = enviro("SPECIALIST_BOOKING_API_MONGODB_DATABASE", "specialist-booking")
	}
	if svc.Collection == "" {
		svc.Collection = enviro("SPECIALIST_BOOKING_API_MONGODB_COLLECTION", "clinics")
	}
	if svc.Timeout == 0 {
		seconds := enviro("SPECIALIST_BOOKING_API_MONGODB_TIMEOUT_SECONDS", "10")
		if parsed, err := strconv.Atoi(seconds); err == nil {
			svc.Timeout = time.Duration(parsed) * time.Second
		} else {
			log.Warn().Str("timeoutSeconds", seconds).Msg("Invalid MongoDB timeout, using default")
			svc.Timeout = 10 * time.Second
		}
	}
	log.Info().
		Str("component", "mongo-service").
		Str("mongodb.host", svc.ServerHost).
		Int("mongodb.port", svc.ServerPort).
		Str("mongodb.database", svc.DbName).
		Str("mongodb.collection", svc.Collection).
		Msg("MongoDB service configured")
	return svc
}

func (m *mongoSvc[DocType]) connect(ctx context.Context) (*mongo.Client, error) {
	ctx, span := m.tracer.Start(ctx, "connect")
	defer span.End()

	client := m.client.Load()
	if client != nil {
		return client, nil
	}
	m.clientLock.Lock()
	defer m.clientLock.Unlock()
	client = m.client.Load()
	if client != nil {
		return client, nil
	}
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	uri := fmt.Sprintf("mongodb://%v:%v", m.ServerHost, m.ServerPort)
	span.SetAttributes(attribute.String("mongodb.uri", uri))
	if len(m.UserName) != 0 {
		uri = fmt.Sprintf("mongodb://%v:%v@%v:%v", m.UserName, m.Password, m.ServerHost, m.ServerPort)
	}
	opts := options.Client()
	opts.Monitor = otelmongo.NewMonitor()
	opts.ApplyURI(uri).SetConnectTimeout(10 * time.Second)
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		span.SetStatus(codes.Error, "MongoDB connection error")
		return nil, err
	}
	m.client.Store(client)
	span.SetStatus(codes.Ok, "MongoDB connection established")
	return client, nil
}

func (m *mongoSvc[DocType]) collection(ctx context.Context) (*mongo.Collection, context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	client, err := m.connect(ctx)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return client.Database(m.DbName).Collection(m.Collection), ctx, cancel, nil
}

func (m *mongoSvc[DocType]) Disconnect(ctx context.Context) error {
	client := m.client.Load()
	if client == nil {
		return nil
	}
	m.clientLock.Lock()
	defer m.clientLock.Unlock()
	client = m.client.Load()
	defer m.client.Store(nil)
	if client != nil {
		return client.Disconnect(ctx)
	}
	return nil
}

func (m *mongoSvc[DocType]) CreateDocument(ctx context.Context, id string, document *DocType) error {
	ctx, span := m.tracer.Start(ctx, "CreateDocument", trace.WithAttributes(attribute.String("mongodb.collection", m.Collection), attribute.String("entry.id", id)))
	defer span.End()
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer cancel()
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {
	case nil:
		span.SetStatus(codes.Error, "Document already exists")
		return ErrConflict
	case mongo.ErrNoDocuments:
	default:
		span.SetStatus(codes.Error, result.Err().Error())
		return result.Err()
	}
	_, err = collection.InsertOne(ctx, document)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "Document inserted")
	return nil
}

func (m *mongoSvc[DocType]) FindDocument(ctx context.Context, id string) (*DocType, error) {
	ctx, span := m.tracer.Start(ctx, "FindDocument", trace.WithAttributes(attribute.String("mongodb.collection", m.Collection), attribute.String("entry.id", id)))
	defer span.End()
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	defer cancel()
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {
	case nil:
	case mongo.ErrNoDocuments:
		span.SetStatus(codes.Error, "Document not found")
		return nil, ErrNotFound
	default:
		span.SetStatus(codes.Error, result.Err().Error())
		return nil, result.Err()
	}
	var document DocType
	if err := result.Decode(&document); err != nil {
		span.SetStatus(codes.Error, "Document decode error")
		return nil, err
	}
	span.SetStatus(codes.Ok, "Document found")
	return &document, nil
}

func (m *mongoSvc[DocType]) UpdateDocument(ctx context.Context, id string, document *DocType) error {
	ctx, span := m.tracer.Start(ctx, "UpdateDocument", trace.WithAttributes(attribute.String("mongodb.collection", m.Collection), attribute.String("entry.id", id)))
	defer span.End()
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer cancel()
	result := collection.FindOneAndReplace(ctx, bson.D{{Key: "id", Value: id}}, document)
	switch result.Err() {
	case nil:
		span.SetStatus(codes.Ok, "Document updated")
		return nil
	case mongo.ErrNoDocuments:
		span.SetStatus(codes.Error, "Document not found")
		return ErrNotFound
	default:
		span.SetStatus(codes.Error, result.Err().Error())
		return result.Err()
	}
}

func (m *mongoSvc[DocType]) DeleteDocument(ctx context.Context, id string) error {
	ctx, span := m.tracer.Start(ctx, "DeleteDocument", trace.WithAttributes(attribute.String("mongodb.collection", m.Collection), attribute.String("entry.id", id)))
	defer span.End()
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer cancel()
	result, err := collection.DeleteOne(ctx, bson.D{{Key: "id", Value: id}})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if result.DeletedCount == 0 {
		span.SetStatus(codes.Error, "Document not found")
		return ErrNotFound
	}
	span.SetStatus(codes.Ok, "Document deleted")
	return nil
}
